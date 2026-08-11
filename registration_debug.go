package main

import (
	"fmt"
	"strings"
)

// sendRegistrationFailureDebug emails a method-level trace for mock/test
// failures only. It intentionally includes no mailbox passwords or secrets.
func sendRegistrationFailureDebug(orderID string, cause error) error {
	if registrarMode() != "mock" && !paystackTestModeEnabled() {
		return nil
	}
	order, err := loadOrderDetail(orderID)
	if err != nil {
		return err
	}
	if order.Status != "provisioning_failed" {
		return nil
	}
	failure := "unknown failure"
	if cause != nil {
		failure = cause.Error()
	}
	for _, event := range order.Events {
		if event.EventType == "registration_debug_sent" && strings.Contains(event.Message, failure) {
			return nil
		}
	}

	lines := []string{
		"LocalBox registration failure debug trace",
		"",
		"Order: " + order.ID,
		"Domain: " + order.Domain,
		"Status: " + order.Status,
		"Failure: " + failure,
		"Registrar reference: " + presentOrMissing(order.RegistrarReference),
		"Cloudflare zone ID: " + presentOrMissing(order.CloudflareZoneID),
		"Cloudflare nameservers: " + presentOrMissing(order.CloudflareNameservers),
		"",
		"METHOD / EVENT TRACE",
	}
	for _, event := range order.Events {
		lines = append(lines, fmt.Sprintf("%s | %s | %s", event.CreatedAt, event.EventType, event.Message))
	}
	lines = append(lines, "", "This diagnostic contains no mailbox password or API credential.")

	if err := sendOrderNotificationEmail(orderEmailMessage{
		To:      setupChecklistRecipient,
		Subject: "LocalBox registration failed: " + order.Domain,
		Body:    strings.Join(lines, "\n"),
	}); err != nil {
		return err
	}
	return appendOrderEvent(order.ID, "registration_debug_sent", "Method-level registration failure trace emailed: "+failure)
}
