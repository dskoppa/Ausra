package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"path/filepath"
	"time"

	bolt "go.etcd.io/bbolt"
)

var deliveryBucket = []byte("github-deliveries")

// The JSON state keeps only recent deliveries for the UI. This database keeps
// every accepted delivery ID so an old redelivery cannot start a second run.
func (a *App) ensureDeliveryLedgerLocked() error {
	if a.deliveryLedger != nil {
		return nil
	}
	path := filepath.Join(filepath.Dir(a.dataFile), "webhook-deliveries.db")
	db, err := bolt.Open(path, 0600, &bolt.Options{Timeout: time.Second})
	if err != nil {
		return fmt.Errorf("open webhook delivery ledger: %w", err)
	}
	if err := db.Update(func(tx *bolt.Tx) error {
		bucket, err := tx.CreateBucketIfNotExists(deliveryBucket)
		if err != nil {
			return err
		}
		// Backfill deployments that previously stored only the latest 1,000
		// deliveries, and reconcile a crash between JSON and ledger writes.
		for _, delivery := range a.state.WebhookDeliveries {
			value, err := json.Marshal(delivery)
			if err != nil {
				return err
			}
			if err := bucket.Put(deliveryKey(delivery.RepositoryID, delivery.ID), value); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		db.Close()
		return fmt.Errorf("initialize webhook delivery ledger: %w", err)
	}
	a.deliveryLedger = db
	return nil
}

func deliveryKey(repoID, deliveryID string) []byte {
	return []byte(repoID + "\x00" + deliveryID)
}

func (a *App) ledgerDeliveryLocked(repoID, deliveryID string) (WebhookDelivery, bool, error) {
	var delivery WebhookDelivery
	var found bool
	err := a.deliveryLedger.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(deliveryBucket).Get(deliveryKey(repoID, deliveryID))
		if value == nil {
			return nil
		}
		found = true
		return json.Unmarshal(value, &delivery)
	})
	return delivery, found, err
}

func (a *App) putLedgerDeliveryLocked(delivery WebhookDelivery) error {
	if a.deliveryLedger == nil {
		return errors.New("webhook delivery ledger is not open")
	}
	value, err := json.Marshal(delivery)
	if err != nil {
		return err
	}
	return a.deliveryLedger.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(deliveryBucket).Put(deliveryKey(delivery.RepositoryID, delivery.ID), value)
	})
}

func (a *App) deleteLedgerDeliveryLocked(repoID, deliveryID string) error {
	return a.deliveryLedger.Update(func(tx *bolt.Tx) error {
		return tx.Bucket(deliveryBucket).Delete(deliveryKey(repoID, deliveryID))
	})
}

func (a *App) closeDeliveryLedger() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.deliveryLedger == nil {
		return nil
	}
	err := a.deliveryLedger.Close()
	a.deliveryLedger = nil
	return err
}

func (a *App) restoreLedgerDeliveryLocked(repoID, deliveryID string, previous WebhookDelivery, existed bool) error {
	if existed {
		return a.putLedgerDeliveryLocked(previous)
	}
	return a.deleteLedgerDeliveryLocked(repoID, deliveryID)
}

func (a *App) reserveLedgerDeliveryLocked(delivery WebhookDelivery) error {
	delivery.Status = "reserved"
	return a.putLedgerDeliveryLocked(delivery)
}

func (a *App) recordLedgerDeliveryLocked(delivery WebhookDelivery) {
	if err := a.putLedgerDeliveryLocked(delivery); err != nil {
		// The JSON state may already include this delivery. Restart backfills
		// the ledger before accepting another webhook.
		log.Fatalf("save webhook delivery ledger: %v", err)
	}
}
