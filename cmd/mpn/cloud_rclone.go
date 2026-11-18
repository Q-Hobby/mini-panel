//go:build cloud

package main

// This file is built only into mpn-cloud (go build -tags cloud ./cmd/mpn), the same program with rclone in it. rclone and its
// backends make the program four times larger and slow every start by about 100 ms, and the panel starts mpn several times for each
// page, so mpn itself is built without them and hands the commands that talk to a storage over to mpn-cloud (cloud_delegate.go).

import (
	"context"
	"fmt"
	"os"

	"mpn/internal/cloudprov"
	"mpn/internal/cloudsync"
	"mpn/internal/remote"
)

// delegateCloud does nothing here: this program is mpn-cloud.
func delegateCloud(string, []string) {}

func openRclone(ctx context.Context, saved cloudprov.Saved) (cloudsync.Remote, context.Context) {
	ctx, err := remote.Setup(ctx, remote.ConfigFile)
	fail(err)
	r, err := remote.Open(ctx, saved.Root())
	fail(err)
	return r, ctx
}

// configureRclone writes the remote to a file of its own and tests it there; only a remote that works replaces the real one.
func configureRclone(ctx context.Context, in cloudprov.Settings) error {
	trial := remote.ConfigFile + ".new"
	trialSettings := remote.SettingsFile + ".new"
	defer os.Remove(trial)
	defer os.Remove(trialSettings)
	_ = os.Remove(trial)
	ctx, err := remote.Setup(ctx, trial)
	if err != nil {
		return err
	}
	if err := remote.Write(in, trialSettings); err != nil {
		return err
	}
	r, err := remote.Open(ctx, cloudprov.Root(in.Bucket, in.Folder))
	if err != nil {
		return fmt.Errorf("cloud-test-connect: %w", err)
	}
	if err := r.Test(ctx, cloudScratch); err != nil {
		return err
	}
	if err := os.Rename(trial, remote.ConfigFile); err != nil {
		return err
	}
	return os.Rename(trialSettings, remote.SettingsFile)
}
