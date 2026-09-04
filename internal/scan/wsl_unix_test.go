package scan

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// setWSLVersion points wslVersionFile at a fixture and resets the cached
// detection result, so tests are hermetic and do not depend on the host
// kernel. Returns a cleanup func.
func setWSLVersion(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "version")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := wslVersionFile
	wslVersionFile = p
	wslOnce = sync.Once{}
	wslBool = false
	t.Cleanup(func() {
		wslVersionFile = old
		wslOnce = sync.Once{}
		wslBool = false
	})
}

func TestWSLDetection(t *testing.T) {
	cases := []struct {
		name    string
		version string
		want    bool
	}{
		{name: "wsl2", version: "Linux version 5.15.90.1-microsoft-standard-WSL2 (root@WSL) (gcc (GCC) 11.3.0) #1 SMP", want: true},
		{name: "wsl1", version: "Linux version 4.4.0-19041-Microsoft (Microsoft@Microsoft.com)", want: true},
		{name: "linux", version: "Linux version 6.5.0-1025-azure (buildd@bos01) #26~22.04.1-Ubuntu", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setWSLVersion(t, tc.version)
			if got := WSL(); got != tc.want {
				t.Fatalf("WSL() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestWSLMissingVersionFile(t *testing.T) {
	setWSLVersion(t, "")
	os.Remove(wslVersionFile)
	if WSL() {
		t.Fatal("WSL() = true with no version file, want false")
	}
}

func TestFreeSuppressedOnWSL(t *testing.T) {
	setWSLVersion(t, "Linux version 5.15.90.1-microsoft-standard-WSL2")
	free, err := Free("/")
	if err != nil {
		t.Fatalf("Free(/): %v", err)
	}
	if free != 0 {
		t.Fatalf("Free(/) = %d on WSL, want 0", free)
	}
}

func TestMountsFreeSuppressedOnWSL(t *testing.T) {
	setWSLVersion(t, "Linux version 5.15.90.1-microsoft-standard-WSL2")
	for _, m := range Mounts() {
		if m.Free != 0 {
			t.Fatalf("mount %s (%s) Free = %d on WSL, want 0", m.Path, m.Type, m.Free)
		}
	}
}
