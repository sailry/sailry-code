package product_test

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sailry/sailry-code/internal/product"
	sdkproduct "github.com/sailry/sailry-harness/sdk/product"
)

func TestDesktopReleaseIdentityMatchesCodePackage(t *testing.T) {
	desktop := product.Desktop()
	if product.ID != "code" || desktop.ID != product.ID || desktop.Name != "Sailry Code" || desktop.Version != product.Version {
		t.Fatalf("Code desktop identity = %+v", desktop)
	}
	const releases = "https://api.github.com/repos/sailry/sailry-code/releases"
	const downloads = "https://github.com/sailry/sailry-code/releases/download"
	if desktop.Release.Source != releases || desktop.Release.Downloads != downloads {
		t.Fatalf("Code update sources = %q, %q", desktop.Release.Source, desktop.Release.Downloads)
	}
	// This public value pins the release trust contract, not a signing fixture.
	// Tests neither load a publisher seed nor contact the configured endpoints.
	const publicKey = "6jS/72cAFmabg4/olkkZ4nmClRa2lmpTHZF3M+lhCA4="
	keys := strings.Fields(desktop.Release.PublicKeys)
	if len(keys) != 1 || keys[0] != publicKey {
		t.Fatalf("Code release does not use its expected embedded public key")
	}
	decoded, err := base64.StdEncoding.DecodeString(keys[0])
	if err != nil || len(decoded) != ed25519.PublicKeySize {
		t.Fatalf("Code update public key = %d bytes, %v", len(decoded), err)
	}
	wantPackage := sdkproduct.Package{Name: "Sailry", ID: "ai.sailry.desktop", Executable: "sailry-desktop"}
	if desktop.Release.Package != wantPackage {
		t.Fatalf("Code update lost the compatible installed application identity: %+v", desktop.Release.Package)
	}
	metadata, err := os.ReadFile(filepath.Join("..", "..", "cmd", "sailry-code-desktop", "mygo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var packaged struct {
		Name       string `json:"name"`
		Identifier string `json:"identifier"`
		Version    string `json:"version"`
	}
	if err := json.Unmarshal(metadata, &packaged); err != nil {
		t.Fatal(err)
	}
	if packaged.Name != desktop.Release.Package.Name || packaged.Identifier != desktop.Release.Package.ID || packaged.Version != desktop.Version {
		t.Fatalf("Code package metadata and runtime update identity differ: %+v, %+v", packaged, desktop.Release.Package)
	}
}

func TestDesktopDeclaresCodeProfileMigrationAndExplicitOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	legacy := filepath.Join(home, ".sailry")
	if err := os.Mkdir(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(legacy, "marker")
	if err := os.WriteFile(marker, []byte("legacy profile"), 0o600); err != nil {
		t.Fatal(err)
	}
	desktop := product.Desktop()
	if desktop.DefaultProfile != "~/.sailry-code" || desktop.LegacyProfile != "~/.sailry" {
		t.Fatalf("Code default and legacy profiles = %q, %q", desktop.DefaultProfile, desktop.LegacyProfile)
	}
	profile, err := desktop.DataDir("")
	if err != nil || profile != filepath.Join(home, ".sailry-code") {
		t.Fatalf("Code data directory = %q, %v", profile, err)
	}
	override := filepath.Join(home, "explicit-profile")
	if got, err := desktop.DataDir(override); err != nil || got != override {
		t.Fatalf("explicit data directory = %q, %v", got, err)
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "legacy profile" {
		t.Fatalf("identity resolution changed the legacy profile: %q, %v", contents, err)
	}
	if entries, err := os.ReadDir(home); err != nil || len(entries) != 1 || entries[0].Name() != ".sailry" {
		t.Fatalf("identity resolution unexpectedly created or migrated profile files: %v, %v", entries, err)
	}
}

func TestExplicitDataDirectoryDoesNotRequireHome(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	override := filepath.Join(t.TempDir(), "isolated-profile")
	if got, err := product.Desktop().DataDir(override); err != nil || got != override {
		t.Fatalf("explicit data directory without a home = %q, %v", got, err)
	}
}

func TestDesktopUsesCodeHostFeedWithCompatibleInstalledHost(t *testing.T) {
	host := product.Desktop().HostRelease
	if host != sdkproduct.CodeHost() || !host.Published() || host.Independent() {
		t.Fatal("Code desktop did not select its trusted successor Host binding")
	}
	if host.Releases() != "https://api.github.com/repos/sailry/sailry-code/releases" ||
		host.Downloads() != "https://github.com/sailry/sailry-code/releases/download" {
		t.Fatalf("Code Host sources = %q, %q", host.Releases(), host.Downloads())
	}
	pkg := host.Package()
	if pkg.Name() != "sailry" || pkg.Binary() != "sailry-host" || pkg.Profile() != ".sailry-host" || pkg.LaunchAgent() != "ai.sailry.host" {
		t.Fatalf("Code Host changed the installed binary, data or service namespace: %q, %q, %q, %q",
			pkg.Name(), pkg.Binary(), pkg.Profile(), pkg.LaunchAgent())
	}
}
