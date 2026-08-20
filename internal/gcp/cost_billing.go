package gcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"cloud.google.com/go/bigquery"
	"google.golang.org/api/iterator"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
	"github.com/asbrodova/aura-tracker-gcp/ports"
)

var (
	costProjectIDRE = regexp.MustCompile(`^[a-z][a-z0-9-]{4,28}[a-z0-9]$`)
	costDatasetIDRE = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	costTableIDRE   = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_-]*$`)
	costFieldIDRE   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

const detailedBillingTablePrefix = "gcp_billing_export_resource_v1_"

type costContributorRow struct {
	Dimension       string  `bigquery:"dimension"`
	DimensionKey    string  `bigquery:"dimension_key"`
	Service         string  `bigquery:"service"`
	SKU             string  `bigquery:"sku"`
	Resource        string  `bigquery:"resource"`
	Location        string  `bigquery:"location"`
	UsageUnit       string  `bigquery:"usage_unit"`
	CurrentGross    float64 `bigquery:"current_gross"`
	CurrentCredits  float64 `bigquery:"current_credits"`
	CurrentNet      float64 `bigquery:"current_net"`
	CurrentUsage    float64 `bigquery:"current_usage"`
	BaselineGross   float64 `bigquery:"baseline_gross"`
	BaselineCredits float64 `bigquery:"baseline_credits"`
	BaselineNet     float64 `bigquery:"baseline_net"`
	BaselineUsage   float64 `bigquery:"baseline_usage"`
}

type costHistoryRow struct {
	RecordKind        string  `bigquery:"record_kind"`
	Day               string  `bigquery:"day"`
	Resource          string  `bigquery:"resource"`
	Service           string  `bigquery:"service"`
	FirstSeen         string  `bigquery:"first_seen"`
	GrossCost         float64 `bigquery:"gross_cost"`
	Credits           float64 `bigquery:"credits"`
	NetCost           float64 `bigquery:"net_cost"`
	DataThrough       string  `bigquery:"data_through"`
	ExportStart       string  `bigquery:"export_start"`
	Currency          string  `bigquery:"currency"`
	CurrencyCount     int64   `bigquery:"currency_count"`
	AttributedNetCost float64 `bigquery:"attributed_net_cost"`
	TotalNetCost      float64 `bigquery:"total_net_cost"`
}

// CollectCostFacts reads aggregated facts from a Cloud Billing detailed usage
// export. The query is fixed by the server; callers can only supply parameters.
func (a *gcpAdapter) CollectCostFacts(ctx context.Context, req models.CollectCostFactsRequest) (models.BillingCostFacts, error) {
	const op = "cost.CollectCostFacts"
	if !a.enableCostReasoning {
		return models.BillingCostFacts{}, fmt.Errorf("%s: cost reasoning is not configured", op)
	}
	if err := a.rateWait(ctx, op); err != nil {
		return models.BillingCostFacts{}, err
	}
	req.ProjectID = strings.TrimSpace(req.ProjectID)
	if !costProjectIDRE.MatchString(req.ProjectID) {
		return models.BillingCostFacts{}, fmt.Errorf("%s: invalid project_id %q", op, req.ProjectID)
	}
	source, err := a.costSourceForProject(req.ProjectID)
	if err != nil {
		return models.BillingCostFacts{}, err
	}
	if source.client == nil {
		return models.BillingCostFacts{}, fmt.Errorf("%s: cost source client is not configured", op)
	}
	currentStart, currentEnd, baselineStart, baselineEnd, historyStart, err := parseCostFactTimes(req)
	if err != nil {
		return models.BillingCostFacts{}, fmt.Errorf("%s: %w", op, err)
	}
	if _, err := time.LoadLocation(req.Timezone); err != nil {
		return models.BillingCostFacts{}, fmt.Errorf("%s: invalid timezone %q: %w", op, req.Timezone, err)
	}
	if req.MaxResults <= 0 {
		req.MaxResults = 10
	}
	if req.MaxResults > 25 {
		return models.BillingCostFacts{}, fmt.Errorf("%s: max_results must be at most 25", op)
	}

	ctx, cancel := a.withTimeout(ctx)
	defer cancel()
	tableID, meta, err := a.resolveDetailedBillingTable(ctx, source)
	if err != nil {
		return models.BillingCostFacts{}, err
	}
	tableRef := costTableReference(source, tableID)
	partitionPredicate := costPartitionPredicate(meta)

	contributorParams := []bigquery.QueryParameter{
		{Name: "project_id", Value: req.ProjectID},
		{Name: "current_start", Value: currentStart},
		{Name: "current_end", Value: currentEnd},
		{Name: "baseline_start", Value: baselineStart},
		{Name: "baseline_end", Value: baselineEnd},
		{Name: "max_rows", Value: int64(req.MaxResults)},
	}
	if partitionPredicate != "" {
		contributorParams = append(contributorParams, bigquery.QueryParameter{Name: "partition_start", Value: baselineStart})
	}

	facts := models.BillingCostFacts{DetailedExport: true, Daily: []models.HistoricalCost{}, Facts: []models.CostFact{}, FirstSeen: []models.ResourceFirstSeen{}, Warnings: []string{}}
	contributorSQL := buildCostContributorSQL(tableRef, partitionPredicate)
	bytes, err := a.executeCostQuery(ctx, source, contributorSQL, contributorParams, func(it *bigquery.RowIterator) error {
		for {
			var row costContributorRow
			if err := it.Next(&row); err != nil {
				if errors.Is(err, iterator.Done) {
					return nil
				}
				return err
			}
			facts.Facts = append(facts.Facts, models.CostFact{
				Dimension: row.Dimension, Key: row.DimensionKey, Service: row.Service,
				SKU: row.SKU, Resource: row.Resource, Location: row.Location, UsageUnit: row.UsageUnit,
				Current:  models.CostMeasure{GrossCost: row.CurrentGross, Credits: row.CurrentCredits, NetCost: row.CurrentNet, Usage: row.CurrentUsage},
				Baseline: models.CostMeasure{GrossCost: row.BaselineGross, Credits: row.BaselineCredits, NetCost: row.BaselineNet, Usage: row.BaselineUsage},
			})
		}
	})
	if err != nil {
		return models.BillingCostFacts{}, wrapGCPError(op+".contributors", err)
	}
	facts.BytesProcessed += bytes

	historyParams := []bigquery.QueryParameter{
		{Name: "project_id", Value: req.ProjectID},
		{Name: "current_start", Value: currentStart},
		{Name: "current_end", Value: currentEnd},
		{Name: "history_start", Value: historyStart},
		{Name: "timezone", Value: req.Timezone},
		{Name: "max_rows", Value: int64(req.MaxResults)},
	}
	if partitionPredicate != "" {
		historyParams = append(historyParams, bigquery.QueryParameter{Name: "partition_start", Value: historyStart})
	}
	historySQL := buildCostHistorySQL(tableRef, partitionPredicate)
	bytes, err = a.executeCostQuery(ctx, source, historySQL, historyParams, func(it *bigquery.RowIterator) error {
		for {
			var row costHistoryRow
			if err := it.Next(&row); err != nil {
				if errors.Is(err, iterator.Done) {
					return nil
				}
				return err
			}
			switch row.RecordKind {
			case "daily":
				facts.Daily = append(facts.Daily, models.HistoricalCost{Date: row.Day, GrossCost: row.GrossCost, Credits: row.Credits, NetCost: row.NetCost})
			case "resource":
				facts.FirstSeen = append(facts.FirstSeen, models.ResourceFirstSeen{Resource: row.Resource, Service: row.Service, FirstSeen: row.FirstSeen, CurrentCost: row.NetCost})
			case "meta":
				facts.DataThrough = row.DataThrough
				facts.ExportStart = row.ExportStart
				facts.Currency = row.Currency
				facts.CurrencyCount = row.CurrencyCount
				facts.ResourceAttributedNetCost = row.AttributedNetCost
				facts.TotalNetCostInHistory = row.TotalNetCost
				if row.CurrencyCount > 1 {
					facts.Warnings = append(facts.Warnings, "Billing export contains multiple currencies; amounts were not converted.")
				}
			}
		}
	})
	if err != nil {
		return models.BillingCostFacts{}, wrapGCPError(op+".history", err)
	}
	facts.BytesProcessed += bytes
	sort.Slice(facts.Daily, func(i, j int) bool { return facts.Daily[i].Date < facts.Daily[j].Date })
	sort.Slice(facts.FirstSeen, func(i, j int) bool {
		if facts.FirstSeen[i].FirstSeen == facts.FirstSeen[j].FirstSeen {
			return facts.FirstSeen[i].Resource < facts.FirstSeen[j].Resource
		}
		return facts.FirstSeen[i].FirstSeen < facts.FirstSeen[j].FirstSeen
	})
	return facts, nil
}

func (a *gcpAdapter) costSourceForProject(projectID string) (*costSource, error) {
	source, ok := a.costSources[projectID]
	if !ok || source == nil {
		return nil, &ports.CostSourceNotConfiguredError{ProjectID: projectID}
	}
	return source, nil
}

func costTableReference(source *costSource, tableID string) string {
	return fmt.Sprintf("`%s.%s.%s`", source.config.ExportProjectID, source.config.Dataset, tableID)
}

func costPartitionPredicate(meta *bigquery.TableMetadata) string {
	if meta == nil || meta.TimePartitioning == nil {
		return ""
	}
	field := meta.TimePartitioning.Field
	if field == "" {
		return "AND _PARTITIONTIME >= TIMESTAMP(@partition_start)"
	}
	if !costFieldIDRE.MatchString(field) {
		return ""
	}
	return fmt.Sprintf("AND DATE(`%s`) >= DATE(@partition_start)", field)
}

func parseCostFactTimes(req models.CollectCostFactsRequest) (time.Time, time.Time, time.Time, time.Time, time.Time, error) {
	values := []string{req.CurrentStart, req.CurrentEnd, req.BaselineStart, req.BaselineEnd, req.HistoryStart}
	parsed := make([]time.Time, len(values))
	for i, value := range values {
		v, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}, time.Time{}, time.Time{}, time.Time{}, time.Time{}, fmt.Errorf("invalid RFC3339 time %q", value)
		}
		parsed[i] = v
	}
	if !parsed[0].Before(parsed[1]) || !parsed[2].Before(parsed[3]) ||
		parsed[4].After(parsed[2]) || parsed[3].After(parsed[0]) {
		return time.Time{}, time.Time{}, time.Time{}, time.Time{}, time.Time{}, errors.New("invalid or overlapping cost windows")
	}
	return parsed[0], parsed[1], parsed[2], parsed[3], parsed[4], nil
}

func (a *gcpAdapter) resolveDetailedBillingTable(ctx context.Context, source *costSource) (string, *bigquery.TableMetadata, error) {
	if source == nil || source.client == nil {
		return "", nil, errors.New("cost.resolveTable: cost source is not configured")
	}
	if !costProjectIDRE.MatchString(source.config.ExportProjectID) {
		return "", nil, fmt.Errorf("cost.resolveTable: invalid export project ID %q", source.config.ExportProjectID)
	}
	if len(source.config.Dataset) > 1024 || !costDatasetIDRE.MatchString(source.config.Dataset) {
		return "", nil, fmt.Errorf("cost.resolveTable: invalid dataset ID %q", source.config.Dataset)
	}
	dataset := source.client.DatasetInProject(source.config.ExportProjectID, source.config.Dataset)
	tableID := source.config.Table
	if tableID == "" {
		var matches []string
		it := dataset.Tables(ctx)
		for {
			table, err := it.Next()
			if errors.Is(err, iterator.Done) {
				break
			}
			if err != nil {
				return "", nil, wrapGCPError("cost.resolveTable.list", err)
			}
			if strings.HasPrefix(table.TableID, detailedBillingTablePrefix) {
				matches = append(matches, table.TableID)
			}
		}
		sort.Strings(matches)
		switch len(matches) {
		case 0:
			return "", nil, fmt.Errorf("cost.resolveTable: no detailed billing export table with prefix %q found in %s.%s", detailedBillingTablePrefix, source.config.ExportProjectID, source.config.Dataset)
		case 1:
			tableID = matches[0]
		default:
			return "", nil, fmt.Errorf("cost.resolveTable: multiple detailed billing tables found; configure cost_reasoning.table explicitly")
		}
	}
	if len(tableID) > 1024 || !costTableIDRE.MatchString(tableID) || !strings.HasPrefix(tableID, detailedBillingTablePrefix) {
		return "", nil, fmt.Errorf("cost.resolveTable: invalid detailed billing table ID %q", tableID)
	}
	meta, err := dataset.Table(tableID).Metadata(ctx)
	if err != nil {
		return "", nil, wrapGCPError("cost.resolveTable.metadata", err)
	}
	if !schemaHasTopLevelField(meta.Schema, "resource") || !schemaHasTopLevelField(meta.Schema, "credits") {
		return "", nil, fmt.Errorf("cost.resolveTable: %s is not a compatible detailed billing export", tableID)
	}
	return tableID, meta, nil
}

func schemaHasTopLevelField(schema bigquery.Schema, name string) bool {
	for _, field := range schema {
		if field.Name == name {
			return true
		}
	}
	return false
}

func (a *gcpAdapter) executeCostQuery(ctx context.Context, source *costSource, sql string, params []bigquery.QueryParameter, consume func(*bigquery.RowIterator) error) (int64, error) {
	if source == nil || source.client == nil {
		return 0, errors.New("cost query source is not configured")
	}
	dry := source.client.Query(sql)
	dry.Parameters = params
	dry.DryRun = true
	dry.UseLegacySQL = false
	dryJob, err := dry.Run(ctx)
	if err != nil {
		return 0, err
	}
	dryStatus := dryJob.LastStatus()
	if dryStatus == nil {
		return 0, errors.New("cost query dry run returned no status")
	}
	if err := dryStatus.Err(); err != nil {
		return 0, err
	}
	estimated := int64(0)
	if dryStatus.Statistics != nil {
		estimated = dryStatus.Statistics.TotalBytesProcessed
	}
	if estimated > source.config.MaxBytesBilled {
		return 0, fmt.Errorf("cost query would process %d bytes, exceeding max_bytes_billed=%d", estimated, source.config.MaxBytesBilled)
	}

	query := source.client.Query(sql)
	query.Parameters = params
	query.UseLegacySQL = false
	query.MaxBytesBilled = source.config.MaxBytesBilled
	query.JobTimeout = a.callTimeout
	query.Labels = map[string]string{"aura_tracker": "cost_reasoning"}
	job, err := query.Run(ctx)
	if err != nil {
		return 0, err
	}
	status, err := job.Wait(ctx)
	if err != nil {
		return 0, err
	}
	if err := status.Err(); err != nil {
		return 0, err
	}
	it, err := job.Read(ctx)
	if err != nil {
		return 0, err
	}
	if err := consume(it); err != nil {
		return 0, err
	}
	processed := estimated
	if status.Statistics != nil {
		processed = status.Statistics.TotalBytesProcessed
	}
	return processed, nil
}

func buildCostContributorSQL(tableRef, partitionPredicate string) string {
	return fmt.Sprintf(`
WITH source AS (
  SELECT
	COALESCE(service.description, '') AS service,
	COALESCE(sku.description, '') AS sku,
    COALESCE(NULLIF(resource.global_name, ''), NULLIF(resource.name, ''), '') AS resource,
    COALESCE(location.location, '') AS location,
    COALESCE(usage.pricing_unit, usage.unit, '') AS usage_unit,
    COALESCE(usage.amount_in_pricing_units, usage.amount, 0) AS usage_amount,
    cost AS gross_cost,
    IFNULL((SELECT SUM(c.amount) FROM UNNEST(credits) c), 0) AS credits,
    cost + IFNULL((SELECT SUM(c.amount) FROM UNNEST(credits) c), 0) AS net_cost,
    CASE
      WHEN usage_start_time >= @current_start AND usage_start_time < @current_end THEN 'current'
      WHEN usage_start_time >= @baseline_start AND usage_start_time < @baseline_end THEN 'baseline'
      ELSE 'outside'
    END AS period
  FROM %s
  WHERE project.id = @project_id
    AND COALESCE(cost_type, 'regular') = 'regular'
    AND usage_start_time >= @baseline_start
    AND usage_start_time < @current_end
    %s
), expanded AS (
  SELECT d.*, gross_cost, credits, net_cost, period
  FROM source
  CROSS JOIN UNNEST([
    STRUCT('total' AS dimension, 'total' AS dimension_key, '' AS service, '' AS sku, '' AS resource, '' AS location, '' AS usage_unit, 0.0 AS usage_amount),
    STRUCT('service', service, service, '', '', '', '', 0.0),
    STRUCT('sku', CONCAT(service, ' / ', sku), service, sku, '', '', '', 0.0),
    STRUCT('resource', resource, service, '', resource, location, '', 0.0),
    STRUCT('resource_sku', CONCAT(resource, ' / ', sku, ' [', usage_unit, ']'), service, sku, resource, location, usage_unit, usage_amount),
    STRUCT('location', location, '', '', '', location, '', 0.0)
  ]) d
  WHERE period != 'outside' AND d.dimension_key != ''
), aggregated AS (
  SELECT
    dimension, dimension_key, service, sku, resource, location, usage_unit,
    SUM(IF(period = 'current', gross_cost, 0)) AS current_gross,
    SUM(IF(period = 'current', credits, 0)) AS current_credits,
    SUM(IF(period = 'current', net_cost, 0)) AS current_net,
    SUM(IF(period = 'current', usage_amount, 0)) AS current_usage,
    SUM(IF(period = 'baseline', gross_cost, 0)) AS baseline_gross,
    SUM(IF(period = 'baseline', credits, 0)) AS baseline_credits,
    SUM(IF(period = 'baseline', net_cost, 0)) AS baseline_net,
    SUM(IF(period = 'baseline', usage_amount, 0)) AS baseline_usage
  FROM expanded
  GROUP BY dimension, dimension_key, service, sku, resource, location, usage_unit
), ranked AS (
  SELECT *,
    ROW_NUMBER() OVER (PARTITION BY dimension ORDER BY current_net DESC, dimension_key) AS spend_rank,
    ROW_NUMBER() OVER (PARTITION BY dimension ORDER BY (current_net - baseline_net) DESC, dimension_key) AS increase_rank,
    ROW_NUMBER() OVER (PARTITION BY dimension ORDER BY ABS(current_net - baseline_net) DESC, dimension_key) AS change_rank
  FROM aggregated
)
SELECT * EXCEPT(spend_rank, increase_rank, change_rank)
FROM ranked
WHERE spend_rank <= @max_rows OR increase_rank <= @max_rows OR change_rank <= @max_rows
ORDER BY dimension, change_rank, increase_rank, spend_rank, dimension_key`, tableRef, partitionPredicate)
}

func buildCostHistorySQL(tableRef, partitionPredicate string) string {
	return fmt.Sprintf(`
WITH source AS (
  SELECT
    usage_start_time,
    usage_end_time,
    export_time,
    FORMAT_DATE('%%F', DATE(usage_start_time, @timezone)) AS day,
	COALESCE(service.description, '') AS service,
    COALESCE(NULLIF(resource.global_name, ''), NULLIF(resource.name, ''), '') AS resource,
    currency,
    cost AS gross_cost,
    IFNULL((SELECT SUM(c.amount) FROM UNNEST(credits) c), 0) AS credits,
    cost + IFNULL((SELECT SUM(c.amount) FROM UNNEST(credits) c), 0) AS net_cost
  FROM %s
  WHERE project.id = @project_id
    AND COALESCE(cost_type, 'regular') = 'regular'
    AND usage_start_time >= @history_start
    AND usage_start_time < @current_end
    %s
), daily AS (
  SELECT
    'daily' AS record_kind, day, '' AS resource, '' AS service, '' AS first_seen,
    SUM(gross_cost) AS gross_cost, SUM(credits) AS credits, SUM(net_cost) AS net_cost,
    '' AS data_through, '' AS export_start, '' AS currency, 0 AS currency_count,
    0.0 AS attributed_net_cost, 0.0 AS total_net_cost
  FROM source GROUP BY day
), resource_rollup AS (
  SELECT
    resource,
    ANY_VALUE(service) AS service,
    MIN(usage_start_time) AS first_seen_time,
    SUM(IF(usage_start_time >= @current_start AND usage_start_time < @current_end, net_cost, 0)) AS current_net
  FROM source
  WHERE resource != ''
  GROUP BY resource
), resources AS (
  SELECT
    'resource' AS record_kind, '' AS day, resource, service,
    FORMAT_TIMESTAMP('%%FT%%TZ', first_seen_time) AS first_seen,
    0.0 AS gross_cost, 0.0 AS credits,
    current_net AS net_cost,
    '' AS data_through, '' AS export_start, '' AS currency, 0 AS currency_count,
    0.0 AS attributed_net_cost, 0.0 AS total_net_cost
  FROM resource_rollup
  WHERE first_seen_time >= @current_start AND first_seen_time < @current_end AND current_net > 0
  QUALIFY ROW_NUMBER() OVER (ORDER BY current_net DESC, resource) <= @max_rows
), metadata AS (
  SELECT
    'meta' AS record_kind, '' AS day, '' AS resource, '' AS service, '' AS first_seen,
    0.0 AS gross_cost, 0.0 AS credits, 0.0 AS net_cost,
		COALESCE(FORMAT_TIMESTAMP('%%FT%%TZ', MAX(usage_end_time)), '') AS data_through,
		COALESCE(FORMAT_TIMESTAMP('%%FT%%TZ', MIN(usage_start_time)), '') AS export_start,
		COALESCE(ARRAY_AGG(currency IGNORE NULLS LIMIT 1)[SAFE_OFFSET(0)], '') AS currency,
    COUNT(DISTINCT currency) AS currency_count,
	IFNULL(SUM(IF(resource != '', net_cost, 0)), 0) AS attributed_net_cost,
	IFNULL(SUM(net_cost), 0) AS total_net_cost
  FROM source
)
SELECT * FROM daily
UNION ALL SELECT * FROM resources
UNION ALL SELECT * FROM metadata
ORDER BY record_kind, day, resource`, tableRef, partitionPredicate)
}
