package gcp

import "testing"

func TestNeededClientsForIncidentModule(t *testing.T) {
	t.Parallel()
	needed := neededClients(map[string]bool{"incident": true})
	for _, client := range []clientKey{
		clientRunSvc,
		clientRunRevisions,
		clientLogAdmin,
		clientMetric,
		clientPubSub,
		clientSQLAdmin,
		clientVPCAccess,
	} {
		if !needed[client] {
			t.Errorf("incident module did not initialize %s", client)
		}
	}
}

func TestAlwaysOnResourcesInitializeCloudRunRevisionClients(t *testing.T) {
	t.Parallel()
	needed := neededClients(map[string]bool{})
	if !needed[clientRunSvc] || !needed[clientRunRevisions] {
		t.Fatalf("resource clients missing: run=%v revisions=%v", needed[clientRunSvc], needed[clientRunRevisions])
	}
}

func TestNeededClientsForCostModule(t *testing.T) {
	t.Parallel()
	needed := neededClients(map[string]bool{"cost": true})
	for _, client := range []clientKey{clientBQ, clientMetric, clientAsset} {
		if !needed[client] {
			t.Errorf("cost module did not initialize %s", client)
		}
	}
}

func TestNeededClientsForSecurityModule(t *testing.T) {
	t.Parallel()
	needed := neededClients(map[string]bool{"security": true})
	for _, client := range []clientKey{
		clientAsset,
		clientIAMAdmin,
		clientSecretMgr,
		clientRunSvc,
		clientFunctionsV1,
		clientCompute,
		clientClusterMgr,
		clientCRMv3,
		clientIAMv2,
		clientGKEHub,
	} {
		if !needed[client] {
			t.Errorf("security module did not initialize %s", client)
		}
	}
}
