package localmode

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// networkProbe prints whether DNS, an internet address, and a loopback server
// in the same session can be reached.
const networkProbe = `import http.server, socket, threading
def reach(host, port):
    try:
        socket.create_connection((host, port), 3).close()
        return "reached"
    except OSError:
        return "denied"
try:
    socket.getaddrinfo("example.com", 443)
    dns = "reached"
except OSError:
    dns = "denied"
server = http.server.HTTPServer(("127.0.0.1", 0), http.server.SimpleHTTPRequestHandler)
threading.Thread(target=server.serve_forever, daemon=True).start()
print("dns", dns, "tcp", reach("1.1.1.1", 443), "loopback", reach("127.0.0.1", server.server_address[1]))
`

type egressSession struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	Network struct {
		Egress  string `json:"egress"`
		Applied string `json:"applied"`
	} `json:"network"`
}

func requireLocalModeSandboxEgress(t *testing.T, binary string, env []string, dir string) {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		return runVolcanoLocalModeE2EStdout(t, binary, env, dir, append([]string{"sandboxes"}, args...)...)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Minute)
	defer cancel()
	created := captureLocalModeCommand(ctx, binary, env, dir, "sandboxes", "run", "--preset", "python3.12", "--duration", "300", "--egress", "deny_all", "--json")
	if created.err != nil {
		// Published images can predate the network policy; Hosting's candidate-image lane must accept it.
		require.NotEqual(t, "1", os.Getenv("VOLCANO_E2E_REQUIRE_SANDBOX_NETWORK"), "candidate image must accept the network policy: %s", created.stderr)
		require.Contains(t, created.stderr, "HTTP 400", "an image without the policy refuses the unknown field")
		return
	}
	var session egressSession
	require.NoError(t, json.Unmarshal([]byte(created.stdout), &session), created.stdout)
	t.Cleanup(func() {
		_, _ = runVolcanoLocalModeE2EAllowFailure(t, binary, env, dir, "sandboxes", "terminate", session.ID)
	})
	assert.Equal(t, "deny_all", session.Network.Egress)

	assert.Contains(t, run("exec", "--preset", "python3.12", "--", "python", "-c", networkProbe), "dns reached tcp reached loopback reached")
	assert.Contains(t, run("exec", "--preset", "python3.12", "--egress", "deny_all", "--", "python", "-c", networkProbe), "dns denied tcp denied loopback reached")

	require.EventuallyWithT(t, func(c *assert.CollectT) {
		read := captureLocalModeCommand(t.Context(), binary, env, dir, "sandboxes", "get", session.ID, "--json")
		if !assert.NoError(c, read.err, read.stderr) || !assert.NoError(c, json.Unmarshal([]byte(read.stdout), &session), read.stdout) {
			return
		}
		assert.Equal(c, "running", session.State)
	}, 2*time.Minute, 2*time.Second)
	assert.Equal(t, "deny_all", session.Network.Applied, "a running session reports its observed network")
	assert.Contains(t, run("exec", session.ID, "--", "python", "-c", networkProbe), "dns denied tcp denied loopback reached")
}
