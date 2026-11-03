package config

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestMasterKeyRequired(t *testing.T) {
	if _, err := Load(env(nil)); err == nil {
		t.Fatal("clé maître absente: erreur attendue")
	}
	if _, err := Load(env(map[string]string{"LEASH_MASTER_KEY": "!!!"})); err == nil {
		t.Fatal("base64 invalide: erreur attendue")
	}
	short := base64.StdEncoding.EncodeToString([]byte("court"))
	if _, err := Load(env(map[string]string{"LEASH_MASTER_KEY": short})); err == nil || !strings.Contains(err.Error(), "32") {
		t.Fatalf("clé trop courte: %v", err)
	}
}

func TestDefaults(t *testing.T) {
	k := base64.StdEncoding.EncodeToString(make([]byte, 32))
	c, err := Load(env(map[string]string{"LEASH_MASTER_KEY": k}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != ":8080" || c.ApprovalTTL != 30*time.Minute || c.SessionTTL != 12*time.Hour || c.LogRetentionDays != 365 {
		t.Fatalf("défauts inattendus: %+v", c)
	}
}
