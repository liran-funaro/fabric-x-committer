package test

import (
	"io"
	"log"
	"os"
	"os/exec"
	"path"
	"testing"
	"time"

	"github.ibm.com/decentralized-trust-research/scalable-committer/integration/runner"
	"google.golang.org/grpc/grpclog"
)

func TestMain(m *testing.M) {
	grpclog.SetLoggerV2(grpclog.NewLoggerV2(io.Discard, io.Discard, os.Stderr))

	dir, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	buildCmd := exec.Command("make", "build")
	buildCmd.Dir = path.Clean(path.Join(dir, "../.."))
	makeRun := runner.Run(buildCmd, "make", "")
	log.Println("wait")
	select {
	case err = <-makeRun.Wait():
		if err != nil {
			log.Fatal(err)
		}
	case <-time.After(3 * time.Minute):
		makeRun.Signal(os.Kill)
		log.Fatalf("Failed to build binaries")
	}

	os.Exit(m.Run())
}
