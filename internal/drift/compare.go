package drift

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/asbrodova/aura-tracker-gcp/pkg/models"
)

func compareComponent(environmentA, environmentB string, a, b []Resource, complete bool) []models.ResourceDrift {
	pairs, onlyA, onlyB := matchResources(a, b)
	results := make([]models.ResourceDrift, 0, len(pairs)+len(onlyA)+len(onlyB))
	for _, pair := range pairs {
		differences := compareResourceIdentity(pair, environmentA, environmentB)
		differences = append(differences, compareMaps(pair.a.Config, pair.b.Config, environmentA, environmentB)...)
		sort.SliceStable(differences, func(i, j int) bool {
			if importanceRank(differences[i].Importance) != importanceRank(differences[j].Importance) {
				return importanceRank(differences[i].Importance) < importanceRank(differences[j].Importance)
			}
			return differences[i].Path < differences[j].Path
		})
		status := "equivalent"
		summary := fmt.Sprintf("%s has equivalent configuration in %s and %s", pair.a.Name, environmentA, environmentB)
		if len(differences) > 0 {
			status = "different"
			summary = fmt.Sprintf("%s is configured differently between %s and %s", pair.a.Name, environmentA, environmentB)
		}
		location := pair.a.Location
		if location == "" {
			location = pair.b.Location
		} else if pair.b.Location != "" && pair.b.Location != location {
			location = pair.a.Location + " ↔ " + pair.b.Location
		}
		qualifier := pair.a.Qualifier
		if qualifier == "" {
			qualifier = pair.b.Qualifier
		} else if pair.b.Qualifier != "" && pair.b.Qualifier != qualifier {
			qualifier = pair.a.Qualifier + " ↔ " + pair.b.Qualifier
		}
		results = append(results, models.ResourceDrift{
			Component: pair.a.Component, ResourceType: pair.a.ResourceType,
			Name: pair.a.Name, Location: location, Qualifier: qualifier,
			Status: status, Summary: summary, FieldDifferences: differences,
		})
	}
	for _, resource := range onlyA {
		status := "missing_in_environment"
		missingIn := environmentB
		summary := fmt.Sprintf("%s is present in %s and missing in %s", resource.Name, environmentA, environmentB)
		if !complete {
			status, missingIn = "unknown_due_to_coverage", ""
			summary = fmt.Sprintf("%s was observed only in %s, but incomplete coverage prevents a missing-resource conclusion", resource.Name, environmentA)
		}
		results = append(results, models.ResourceDrift{
			Component: resource.Component, ResourceType: resource.ResourceType,
			Name: resource.Name, Location: resource.Location, Qualifier: resource.Qualifier,
			Status: status, MissingIn: missingIn, PresentIn: environmentA, Summary: summary,
		})
	}
	for _, resource := range onlyB {
		status := "missing_in_environment"
		missingIn := environmentA
		summary := fmt.Sprintf("%s is present in %s and missing in %s", resource.Name, environmentB, environmentA)
		if !complete {
			status, missingIn = "unknown_due_to_coverage", ""
			summary = fmt.Sprintf("%s was observed only in %s, but incomplete coverage prevents a missing-resource conclusion", resource.Name, environmentB)
		}
		results = append(results, models.ResourceDrift{
			Component: resource.Component, ResourceType: resource.ResourceType,
			Name: resource.Name, Location: resource.Location, Qualifier: resource.Qualifier,
			Status: status, MissingIn: missingIn, PresentIn: environmentB, Summary: summary,
		})
	}
	sortResourceDrifts(results)
	return results
}

func compareResourceIdentity(pair resourcePair, environmentA, environmentB string) []models.DriftFieldDifference {
	var differences []models.DriftFieldDifference
	for _, field := range []struct {
		path string
		a    string
		b    string
	}{
		{path: "/location", a: pair.a.Location, b: pair.b.Location},
		{path: "/qualifier", a: pair.a.Qualifier, b: pair.b.Qualifier},
	} {
		if field.a == field.b {
			continue
		}
		aPresent, bPresent := field.a != "", field.b != ""
		changeType := "modified"
		if !aPresent {
			changeType = "missing_in_" + environmentA
		} else if !bPresent {
			changeType = "missing_in_" + environmentB
		}
		category, importance := classifyPath(field.path)
		differences = append(differences, models.DriftFieldDifference{
			Path: field.path, ChangeType: changeType, Category: category, Importance: importance,
			Summary: fieldSummary(field.path, environmentA, environmentB, aPresent, bPresent),
			Values: []models.DriftEnvironmentValue{
				{Environment: environmentA, Value: field.a, Present: aPresent},
				{Environment: environmentB, Value: field.b, Present: bPresent},
			},
		})
	}
	return differences
}

type resourcePair struct{ a, b Resource }

func matchResources(a, b []Resource) (pairs []resourcePair, onlyA, onlyB []Resource) {
	groupsA, groupsB := groupResources(a), groupResources(b)
	identities := make(map[string]bool, len(groupsA)+len(groupsB))
	for key := range groupsA {
		identities[key] = true
	}
	for key := range groupsB {
		identities[key] = true
	}
	keys := make([]string, 0, len(identities))
	for key := range identities {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		left, right := groupsA[key], groupsB[key]
		if len(left) == 1 && len(right) == 1 {
			pairs = append(pairs, resourcePair{left[0], right[0]})
			continue
		}
		rightByExact := make(map[string]int, len(right))
		for i := range right {
			rightByExact[right[i].exactIdentity()] = i
		}
		used := make(map[int]bool)
		for _, candidate := range left {
			if index, ok := rightByExact[candidate.exactIdentity()]; ok && !used[index] {
				pairs = append(pairs, resourcePair{candidate, right[index]})
				used[index] = true
			} else {
				onlyA = append(onlyA, candidate)
			}
		}
		for index, candidate := range right {
			if !used[index] {
				onlyB = append(onlyB, candidate)
			}
		}
	}
	return pairs, onlyA, onlyB
}

func groupResources(resources []Resource) map[string][]Resource {
	out := make(map[string][]Resource)
	for _, resource := range resources {
		out[resource.identity()] = append(out[resource.identity()], resource)
	}
	return out
}

func compareMaps(a, b map[string]any, environmentA, environmentB string) []models.DriftFieldDifference {
	var out []models.DriftFieldDifference
	compareValues("", a, true, b, true, environmentA, environmentB, &out)
	sort.SliceStable(out, func(i, j int) bool {
		if importanceRank(out[i].Importance) != importanceRank(out[j].Importance) {
			return importanceRank(out[i].Importance) < importanceRank(out[j].Importance)
		}
		return out[i].Path < out[j].Path
	})
	return out
}

func compareValues(path string, a any, aPresent bool, b any, bPresent bool, environmentA, environmentB string, out *[]models.DriftFieldDifference) {
	if aPresent && bPresent {
		mapA, okA := a.(map[string]any)
		mapB, okB := b.(map[string]any)
		if okA && okB {
			keys := make(map[string]bool, len(mapA)+len(mapB))
			for key := range mapA {
				keys[key] = true
			}
			for key := range mapB {
				keys[key] = true
			}
			ordered := make([]string, 0, len(keys))
			for key := range keys {
				ordered = append(ordered, key)
			}
			sort.Strings(ordered)
			for _, key := range ordered {
				valueA, presentA := mapA[key]
				valueB, presentB := mapB[key]
				compareValues(path+"/"+escapePointer(key), valueA, presentA, valueB, presentB, environmentA, environmentB, out)
			}
			return
		}
		arrayA, okA := a.([]any)
		arrayB, okB := b.([]any)
		if okA && okB {
			compareArrays(path, arrayA, arrayB, environmentA, environmentB, out)
			return
		}
	}
	if aPresent == bPresent && reflect.DeepEqual(a, b) {
		return
	}
	changeType := "modified"
	if !aPresent {
		changeType = "missing_in_" + environmentA
	}
	if !bPresent {
		changeType = "missing_in_" + environmentB
	}
	category, importance := classifyPath(path)
	*out = append(*out, models.DriftFieldDifference{
		Path: path, ChangeType: changeType, Category: category, Importance: importance,
		Summary: fieldSummary(path, environmentA, environmentB, aPresent, bPresent),
		Values: []models.DriftEnvironmentValue{
			{Environment: environmentA, Value: safeDifferenceValue(path, a, aPresent), Present: aPresent},
			{Environment: environmentB, Value: safeDifferenceValue(path, b, bPresent), Present: bPresent},
		},
	})
}

func compareArrays(path string, a, b []any, environmentA, environmentB string, out *[]models.DriftFieldDifference) {
	if identitiesA, okA := semanticArrayIdentities(a); okA {
		if identitiesB, okB := semanticArrayIdentities(b); okB {
			keys := make(map[string]bool, len(identitiesA)+len(identitiesB))
			for key := range identitiesA {
				keys[key] = true
			}
			for key := range identitiesB {
				keys[key] = true
			}
			ordered := make([]string, 0, len(keys))
			for key := range keys {
				ordered = append(ordered, key)
			}
			sort.Strings(ordered)
			for _, key := range ordered {
				valueA, presentA := identitiesA[key]
				valueB, presentB := identitiesB[key]
				compareValues(path+"/"+escapePointer(key), valueA, presentA, valueB, presentB, environmentA, environmentB, out)
			}
			return
		}
	}

	field := strings.ToLower(path[strings.LastIndex(path, "/")+1:])
	if !orderedArrays[field] {
		a, b = removeCommonArrayValues(a, b)
	}
	length := len(a)
	if len(b) > length {
		length = len(b)
	}
	for index := 0; index < length; index++ {
		presentA, presentB := index < len(a), index < len(b)
		var valueA, valueB any
		if presentA {
			valueA = a[index]
		}
		if presentB {
			valueB = b[index]
		}
		compareValues(fmt.Sprintf("%s/%d", path, index), valueA, presentA, valueB, presentB, environmentA, environmentB, out)
	}
}

func semanticArrayIdentities(values []any) (map[string]any, bool) {
	if len(values) == 0 {
		return nil, false
	}
	out := make(map[string]any, len(values))
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		identity := ""
		for _, key := range []string{"name", "id", "path", "tag", "host", "attribute", "type"} {
			if candidate, ok := object[key].(string); ok && candidate != "" {
				identity = key + "=" + candidate
				break
			}
		}
		if identity == "" {
			return nil, false
		}
		if _, duplicate := out[identity]; duplicate {
			return nil, false
		}
		out[identity] = value
	}
	return out, true
}

func removeCommonArrayValues(a, b []any) ([]any, []any) {
	remainingA := append([]any(nil), a...)
	remainingB := append([]any(nil), b...)
	for i := 0; i < len(remainingA); {
		matched := -1
		for j := range remainingB {
			if reflect.DeepEqual(remainingA[i], remainingB[j]) {
				matched = j
				break
			}
		}
		if matched < 0 {
			i++
			continue
		}
		remainingA = append(remainingA[:i], remainingA[i+1:]...)
		remainingB = append(remainingB[:matched], remainingB[matched+1:]...)
	}
	return remainingA, remainingB
}

func safeDifferenceValue(path string, value any, present bool) any {
	if !present {
		return nil
	}
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			out[key] = safeDifferenceValue(path+"/"+escapePointer(key), child, true)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for index, child := range typed {
			out[index] = safeDifferenceValue(fmt.Sprintf("%s/%d", path, index), child, true)
		}
		return out
	}
	lower := strings.ToLower(path)
	leaf := lower
	if index := strings.LastIndex(leaf, "/"); index >= 0 {
		leaf = leaf[index+1:]
	}
	if (strings.Contains(lower, "/env_vars/") && (leaf == "value" || leaf == "literal_value_fingerprint")) ||
		strings.Contains(lower, "/omitted_annotation_fingerprints/") ||
		leaf == "password" || leaf == "token" || leaf == "api_key" ||
		leaf == "private_key" || leaf == "credential" || leaf == "credentials" {
		return "[REDACTED]"
	}
	return value
}

func fieldSummary(path, environmentA, environmentB string, aPresent, bPresent bool) string {
	field := strings.TrimPrefix(path, "/")
	if !aPresent {
		return fmt.Sprintf("%s is missing in %s and configured in %s", field, environmentA, environmentB)
	}
	if !bPresent {
		return fmt.Sprintf("%s is configured in %s and missing in %s", field, environmentA, environmentB)
	}
	return fmt.Sprintf("%s differs between %s and %s", field, environmentA, environmentB)
}

func escapePointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}

func classifyPath(path string) (string, string) {
	lower := strings.ToLower(path)
	for _, token := range []string{"iam", "service_account", "public", "ingress", "private", "workload_identity", "network_policy", "binary_authorization", "secret"} {
		if strings.Contains(lower, token) {
			return "security", "high"
		}
	}
	for _, token := range []string{"network", "subnet", "vpc", "firewall", "ip_cidr"} {
		if strings.Contains(lower, token) {
			return "networking", "medium"
		}
	}
	if strings.Contains(lower, "location") || strings.Contains(lower, "qualifier") {
		return "placement", "medium"
	}
	for _, token := range []string{"image", "runtime", "version", "machine_type"} {
		if strings.Contains(lower, token) {
			return "runtime", "medium"
		}
	}
	for _, token := range []string{"replica", "instance", "autoscal", "resource", "cpu", "memory", "parallel"} {
		if strings.Contains(lower, token) {
			return "scaling", "medium"
		}
	}
	return "configuration", "low"
}

func importanceRank(value string) int {
	switch value {
	case "high":
		return 0
	case "medium":
		return 1
	case "low":
		return 2
	default:
		return 3
	}
}

func sortResourceDrifts(values []models.ResourceDrift) {
	sort.SliceStable(values, func(i, j int) bool {
		rank := func(value models.ResourceDrift) int {
			switch value.Status {
			case "different":
				return 0
			case "missing_in_environment":
				return 1
			case "unknown_due_to_coverage":
				return 2
			default:
				return 3
			}
		}
		if rank(values[i]) != rank(values[j]) {
			return rank(values[i]) < rank(values[j])
		}
		if values[i].Component != values[j].Component {
			return values[i].Component < values[j].Component
		}
		if values[i].ResourceType != values[j].ResourceType {
			return values[i].ResourceType < values[j].ResourceType
		}
		if values[i].Name != values[j].Name {
			return values[i].Name < values[j].Name
		}
		return values[i].Location < values[j].Location
	})
}
