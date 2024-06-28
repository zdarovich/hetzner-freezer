package dump

import (
	"fmt"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
)

const defaultPath = "output"

type ServerDump struct {
	Server      schema.Server
	FloatingIPs []schema.FloatingIP
	SSHKeys     []schema.SSHKey
	Snapshot    schema.Image
}

func NewServerDumpPath(dir, project, serverName, dumpID string) string {
	if len(dir) == 0 {
		dir = defaultPath
	}
	return fmt.Sprintf("%s/%s/%s/%s", dir, project, serverName, dumpID)
}

func NewServerPath(dir, project, serverName string) string {
	if len(dir) == 0 {
		dir = defaultPath
	}
	return fmt.Sprintf("%s/%s/%s", dir, project, serverName)
}
