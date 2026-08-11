package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

var errOrderProvisioningInProgress = errors.New("order provisioning already in progress")

type orderProvisioningLock struct {
	file *os.File
}

func acquireOrderProvisioningLock(orderID string) (*orderProvisioningLock, error) {
	orderID = strings.TrimSpace(orderID)
	if orderID == "" {
		return nil, fmt.Errorf("order ID required")
	}
	lockDir := filepath.Join(filepath.Dir(orderDBFile), "provisioning-locks")
	if err := os.MkdirAll(lockDir, 0770); err != nil {
		return nil, err
	}
	lockPath := filepath.Join(lockDir, orderID+".lock")
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0660)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, errOrderProvisioningInProgress
		}
		return nil, err
	}
	return &orderProvisioningLock{file: file}, nil
}

func (lock *orderProvisioningLock) Release() error {
	if lock == nil || lock.file == nil {
		return nil
	}
	if err := syscall.Flock(int(lock.file.Fd()), syscall.LOCK_UN); err != nil {
		_ = lock.file.Close()
		return err
	}
	return lock.file.Close()
}
