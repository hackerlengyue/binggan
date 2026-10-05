package tunnel

import (
	"context"
	"errors"
	"testing"

	"time.haomen/binggan/v2/internal/capture"
)

func TestStartRequiresCertificateTrustBeforeHelperLaunch(t *testing.T) {
	logs := capture.NewLogbook(t.TempDir())
	defer logs.Close()
	for _, failTrust := range []bool{false, true} {
		t.Run(map[bool]string{false: "trusted", true: "denied"}[failTrust], func(t *testing.T) {
			prepared, launched := false, false
			denied := errors.New("certificate trust denied")
			l, err := start(context.Background(), descriptor(t), logs,
				func(context.Context, capture.Descriptor, *capture.Logbook) error {
					prepared = true
					if failTrust {
						return denied
					}
					return nil
				},
				func(_ context.Context, _, url, token string) error {
					if !prepared || failTrust {
						t.Fatal("elevated helper started before interactive certificate trust")
					}
					launched = true
					client := capture.LocalClient()
					defer client.CloseIdleConnections()
					_, err := readLease(client, url, token)
					return err
				})
			if l != nil {
				defer l.server.Close()
			}
			if failTrust {
				if !errors.Is(err, denied) || launched || l != nil {
					t.Fatalf("denial did not stop startup: %v", err)
				}
			} else if err != nil || !launched || l == nil {
				t.Fatalf("trusted startup failed: %v", err)
			}
		})
	}
}

func TestStartCancellationAfterTrustDoesNotLaunchHelper(t *testing.T) {
	logs := capture.NewLogbook(t.TempDir())
	defer logs.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	l, err := start(ctx, descriptor(t), logs,
		func(context.Context, capture.Descriptor, *capture.Logbook) error { cancel(); return nil },
		func(context.Context, string, string, string) error { t.Fatal("helper started after close"); return nil })
	if l != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled start returned %v, %v", l, err)
	}
}
