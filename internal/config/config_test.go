package config

import (
	"encoding/base64"
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestMasterKeyRequired(t *testing.T) {
	if _, err := Load(env(nil)); err == nil {
		t.Fatal("clé maître absente: erreur attendue")
	}
	if _, err := Load(env(map[string]string{"LEASSH_MASTER_KEY": "!!!"})); err == nil {
		t.Fatal("base64 invalide: erreur attendue")
	}
	short := base64.StdEncoding.EncodeToString([]byte("court"))
	if _, err := Load(env(map[string]string{"LEASSH_MASTER_KEY": short})); err == nil || !strings.Contains(err.Error(), "32") {
		t.Fatalf("clé trop courte: %v", err)
	}
}

func TestDefaults(t *testing.T) {
	k := base64.StdEncoding.EncodeToString(make([]byte, 32))
	c, err := Load(env(map[string]string{"LEASSH_MASTER_KEY": k}))
	if err != nil {
		t.Fatal(err)
	}
	if c.SetupToken != "" {
		t.Fatalf("jeton d'initialisation inattendu: %q", c.SetupToken)
	}
	if c.Listen != ":8080" || c.DataDir != "/data" {
		t.Fatalf("défauts inattendus: %+v", c)
	}
}

func TestSetupTokenFromEnv(t *testing.T) {
	k := base64.StdEncoding.EncodeToString(make([]byte, 32))
	c, err := Load(env(map[string]string{"LEASSH_MASTER_KEY": k, "LEASSH_SETUP_TOKEN": "abc"}))
	if err != nil || c.SetupToken != "abc" {
		t.Fatalf("%v %+v", err, c)
	}
}

func TestLLMVariablesAreRead(t *testing.T) {
	k := base64.StdEncoding.EncodeToString(make([]byte, 32))
	c, err := Load(env(map[string]string{"LEASSH_MASTER_KEY": k, "LLM_BASE_URL": "http://litellm:4000/v1", "LLM_API_KEY": "sk-x", "LLM_MODEL": "gpt-x"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.LLMBaseURL != "http://litellm:4000/v1" || c.LLMAPIKey != "sk-x" || c.LLMModel != "gpt-x" {
		t.Fatalf("variables LLM_* ignorées: %+v", c)
	}
}

func TestLLMBaseURLMustBeHTTP(t *testing.T) {
	k := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if _, err := Load(env(map[string]string{"LEASSH_MASTER_KEY": k, "LLM_BASE_URL": "ftp://x"})); err == nil {
		t.Fatal("LLM_BASE_URL non http(s) acceptée")
	}
}

func TestUpdateCheckEnabledByDefaultAndDisablable(t *testing.T) {
	key := base64.StdEncoding.EncodeToString(make([]byte, 32))
	for val, want := range map[string]bool{"": true, "true": true, "false": false, "0": false} {
		c, err := Load(env(map[string]string{"LEASSH_MASTER_KEY": key, "LEASSH_UPDATE_CHECK": val}))
		if err != nil || c.UpdateCheck != want {
			t.Fatalf("LEASSH_UPDATE_CHECK=%q: %v %v", val, c, err)
		}
	}
	if _, err := Load(env(map[string]string{"LEASSH_MASTER_KEY": key, "LEASSH_UPDATE_CHECK": "peut-être"})); err == nil {
		t.Fatal("valeur invalide acceptée")
	}
}
