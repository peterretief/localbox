package main

import (
	"context"
	"path/filepath"
	"testing"
)

func TestRetryTimedOutDomainProvisioningRetriesOnlyTimeoutFailures(t *testing.T) {
	oldDB := orderDBFile
	oldRetry := retryFailedDomainProvisioningOrder
	orderDBFile = filepath.Join(t.TempDir(), "localbox.db")
	t.Cleanup(func() {
		orderDBFile = oldDB
		retryFailedDomainProvisioningOrder = oldRetry
	})

	insertProvisioningTestOrder(t, "provisioning_failed")
	if err := updateOrderFields("ord_test", "provisioning_failed", "Domain provisioning failed. Post registrar: context deadline exceeded.", nil); err != nil {
		t.Fatal(err)
	}

	var retried []string
	retryFailedDomainProvisioningOrder = func(ctx context.Context, orderID string) error {
		retried = append(retried, orderID)
		return nil
	}

	retryTimedOutDomainProvisioning(context.Background())

	if len(retried) != 1 || retried[0] != "ord_test" {
		t.Fatalf("retried = %v, want [ord_test]", retried)
	}
}

func TestIsTimedOutDomainProvisioningRejectsNonTimeoutFailures(t *testing.T) {
	order := &OrderDetail{
		Events: []OrderEvent{{
			EventType: "provisioning_failed",
			Message:   "Domain provisioning failed. Domain is unavailable.",
		}},
	}

	if isTimedOutDomainProvisioning(order) {
		t.Fatal("expected non-timeout failure not to be retried")
	}
}
