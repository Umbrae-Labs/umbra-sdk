package umbra

import "testing"

func TestNormalizeConfigDerivesSameOriginEndpoints(t *testing.T) {
	cfg, ep, err := normalizeConfig(Config{
		BaseURL:     "https://umbra.example.com/",
		ClientID:    "client",
		RedirectURI: "http://127.0.0.1:1420/auth/callback",
	})
	if err != nil {
		t.Fatalf("normalizeConfig() error = %v", err)
	}
	if cfg.Scope != defaultScope {
		t.Fatalf("scope = %q, want %q", cfg.Scope, defaultScope)
	}
	assertEqual(t, ep.apiBaseURL, "https://umbra.example.com/api/v1")
	assertEqual(t, ep.authorizationEndpoint, "https://umbra.example.com/oauth2/auth")
	assertEqual(t, ep.tokenEndpoint, "https://umbra.example.com/oauth2/token")
	assertEqual(t, ep.revocationEndpoint, "https://umbra.example.com/oauth2/revoke")
}

func TestValidateAddress(t *testing.T) {
	valid := []BackupAddress{
		DBBackup("2026-05-10T20:00:00"),
		FullBackup("v1"),
		GameBackup("minecraft", "v1"),
		AssetBackup("cover-minecraft", "latest"),
	}
	for _, address := range valid {
		if err := ValidateAddress(address); err != nil {
			t.Fatalf("ValidateAddress(%+v) error = %v", address, err)
		}
	}

	invalid := []BackupAddress{
		{Category: CategoryDB, Subject: "x", Version: "v1"},
		{Category: CategoryGame, Subject: "", Version: "v1"},
		{Category: CategoryAsset, Subject: "bad/slash", Version: "v1"},
		{Category: BackupCategory("sync"), Subject: "library", Version: "manifest"},
		{Category: CategoryFull, Version: "bad space"},
	}
	for _, address := range invalid {
		if err := ValidateAddress(address); err == nil {
			t.Fatalf("ValidateAddress(%+v) expected error", address)
		}
	}
}

func assertEqual(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
