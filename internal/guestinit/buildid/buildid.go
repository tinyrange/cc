package buildid

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

var identities struct {
	sync.Mutex
	values map[string]string
}

// Resolve returns a stable cache key for the code that contributes to a
// cross-compiled guest agent, including packages linked into the command.
func Resolve(ctx context.Context, moduleRoot, goos, goarch, commandPackage string) (string, error) {
	key := moduleRoot + "\x00" + goos + "\x00" + goarch + "\x00" + commandPackage
	identities.Lock()
	identity := identities.values[key]
	identities.Unlock()
	if identity != "" {
		return identity, nil
	}

	cmd := exec.CommandContext(ctx, "go", "list", "-export", "-f", "{{.BuildID}}", commandPackage)
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	cmd.Dir = moduleRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("resolve %s/%s guest init build identity: %w\n%s", goos, goarch, err, output)
	}
	buildID := strings.TrimSpace(string(output))
	if buildID == "" {
		return "", fmt.Errorf("%s/%s guest init build identity is empty", goos, goarch)
	}
	sum := sha256.Sum256([]byte(buildID))
	identity = hex.EncodeToString(sum[:16])

	identities.Lock()
	if identities.values == nil {
		identities.values = make(map[string]string)
	}
	identities.values[key] = identity
	identities.Unlock()
	return identity, nil
}
