//go:build integration

// Tests d'intégration contre un vrai sshd (linuxserver/openssh-server) lancé via Docker.
// Lancer : go test -tags integration ./internal/sshexec -run Integration
// L'image est figée par empreinte ; LEASSH_TEST_SSHD_IMAGE permet d'en essayer une autre.
package sshexec

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const defaultSSHDImage = "lscr.io/linuxserver/openssh-server@sha256:aa4e1b3916e8555f24e574e6108b0b89a1b20362b0a11800a71280b45999649e"

// startSSHD lance le conteneur et renvoie la cible (clé hôte non encore épinglée).
func startSSHD(t *testing.T) Target {
	t.Helper()
	if _, err := exec.LookPath("docker"); err != nil {
		t.Fatal("docker indisponible: ce test d'intégration en a besoin")
	}
	image := os.Getenv("LEASSH_TEST_SSHD_IMAGE")
	if image == "" {
		image = defaultSSHDImage
	}
	priv, pub, err := GenerateKey("integration")
	if err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("docker", "run", "-d", "--rm", "-p", "127.0.0.1::2222",
		"-e", "PUBLIC_KEY="+strings.TrimSpace(pub), "-e", "USER_NAME=leassh",
		"-e", "PASSWORD_ACCESS=false", "-e", "SUDO_ACCESS=false", image).CombinedOutput()
	if err != nil {
		t.Fatalf("docker run: %v: %s", err, out)
	}
	id := strings.TrimSpace(string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", id).Run() })

	portOut, err := exec.Command("docker", "port", id, "2222/tcp").Output()
	if err != nil {
		t.Fatalf("docker port: %v", err)
	}
	line := strings.Fields(string(portOut))[0]
	port, err := strconv.Atoi(line[strings.LastIndex(line, ":")+1:])
	if err != nil {
		t.Fatalf("port illisible dans %q", line)
	}
	tg := Target{Host: "127.0.0.1", Port: port, User: "leassh", Auth: Auth{PrivateKeyPEM: priv}}

	deadline := time.Now().Add(90 * time.Second)
	for {
		fp, _, err := ScanHostKey(context.Background(), tg.Host, tg.Port)
		if err == nil {
			tg.Fingerprint = fp
			return tg
		}
		if time.Now().After(deadline) {
			t.Fatalf("sshd injoignable: %v", err)
		}
		time.Sleep(time.Second)
	}
}

func TestIntegrationOpenSSH(t *testing.T) {
	tg := startSSHD(t)
	ctx := context.Background()

	c, err := Dial(ctx, tg)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()

	t.Run("succes", func(t *testing.T) {
		r, err := Run(ctx, c, "echo hello; echo err >&2", 10*time.Second, 1024)
		if err != nil || r.ExitCode != 0 || strings.TrimSpace(r.Stdout) != "hello" || strings.TrimSpace(r.Stderr) != "err" {
			t.Fatalf("%+v %v", r, err)
		}
	})
	t.Run("echec", func(t *testing.T) {
		r, _ := Run(ctx, c, "exit 3", 10*time.Second, 1024)
		if r.ExitCode != 3 {
			t.Fatalf("code retour: %+v", r)
		}
	})
	t.Run("sortie tronquee", func(t *testing.T) {
		r, _ := Run(ctx, c, "head -c 5000 /dev/zero | tr '\\0' a", 10*time.Second, 100)
		if !r.Truncated || len(r.Stdout) != 100 {
			t.Fatalf("troncature: %d %v", len(r.Stdout), r.Truncated)
		}
	})
	t.Run("timeout", func(t *testing.T) {
		r, _ := Run(ctx, c, "sleep 30", 500*time.Millisecond, 100)
		if !r.TimedOut || r.Duration > 10*time.Second {
			t.Fatalf("timeout: %+v", r)
		}
	})
	t.Run("pas de pty", func(t *testing.T) {
		r, _ := Run(ctx, c, "test -t 0 && echo tty || echo notty", 10*time.Second, 100)
		if strings.TrimSpace(r.Stdout) != "notty" {
			t.Fatalf("un terminal a été alloué: %+v", r)
		}
	})
	t.Run("divergence de cle hote", func(t *testing.T) {
		bad := tg
		bad.Fingerprint = "SHA256:autre"
		_, err := Dial(ctx, bad)
		var mm *HostKeyMismatchError
		if !errors.As(err, &mm) || mm.Got != tg.Fingerprint {
			t.Fatalf("divergence attendue: %v", err)
		}
	})
	t.Run("mauvaise cle client", func(t *testing.T) {
		other, _, _ := GenerateKey("x")
		bad := tg
		bad.Auth.PrivateKeyPEM = other
		if _, err := Dial(ctx, bad); err == nil {
			t.Fatal("mauvaise clé acceptée")
		}
	})
}
