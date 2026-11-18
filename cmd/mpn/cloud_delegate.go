//go:build !cloud

package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"

	"mpn/internal/cloudprov"
	"mpn/internal/cloudsync"
)

// mpnCloudPath is the same program built with rclone (see cloud_rclone.go). install.sh and mpn update put it beside mpn.
const mpnCloudPath = "/usr/local/bin/mpn-cloud"

// delegatedCloud are the subcommands of "mpn cloud" that may talk to a storage through rclone.
var delegatedCloud = map[string]bool{"configure": true, "sync": true, "list": true, "fetch": true}

// delegateCloud runs "mpn-cloud cloud <args>" in place of this command when the command needs rclone, and exits with its status.
func delegateCloud(sub string, args []string) {
	if !delegatedCloud[sub] {
		return
	}
	if _, err := os.Stat(mpnCloudPath); err != nil {
		fail(fmt.Errorf("%s is missing: the copy to off-site storage needs it; run mpn panel migrate -apply to download it (release %s)", mpnCloudPath, version))
	}
	cmd := exec.Command(mpnCloudPath, append([]string{"cloud"}, args...)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode())
	}
	fail(err)
	os.Exit(0)
}

func openRclone(context.Context, cloudprov.Saved) (cloudsync.Remote, context.Context) {
	fail(errors.New("this program is built without rclone; run the command through " + mpnCloudPath))
	return nil, nil
}

func configureRclone(context.Context, cloudprov.Settings) error {
	return errors.New("this program is built without rclone; run the command through " + mpnCloudPath)
}
