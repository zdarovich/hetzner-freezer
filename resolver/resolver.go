package resolver

import (
	"context"
	"errors"
	"fmt"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/hetznercloud/hcloud-go/v2/hcloud/schema"
	"github.com/samber/lo"
	"github.com/sirupsen/logrus"
	"hetzner-freezer/dump"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

const pollingInterval = 5 * time.Second
const pollingDeadline = 10 * time.Minute

const cloudInitContent = `ip addr add 0.0.0.0/32 dev eth0`

type Resolver interface {
	CreateServerDump(ctx context.Context, serverName string) (string, error)
	FreezeServer(ctx context.Context, serverName string) (string, error)
	UnfreezeServer(ctx context.Context, serverName string, serverDumpID string) error
}

type resolverService struct {
	project   string
	directory string
	client    *hcloud.Client
	logger    *logrus.Logger
}

func (p *resolverService) UnfreezeServer(ctx context.Context, serverName string, serverDumpID string) error {
	if len(serverDumpID) == 0 {
		// find latest dump from directory
		path := dump.NewServerPath(p.directory, p.project, serverName)
		dirNames, err := GetDirectoriesNames(path)
		if err != nil {
			return fmt.Errorf("failed to get directories: %w", err)
		}
		dirNamesNums := lo.Map(dirNames, func(item string, index int) int {
			num, err := strconv.Atoi(item)
			if err != nil {
				return 0
			}
			return num
		})
		if len(dirNamesNums) == 0 {
			return errors.New("no dumps found. please create a dump first running 'freeze' command")
		}
		slices.Sort(dirNamesNums)
		serverDumpID = strconv.Itoa(dirNamesNums[len(dirNamesNums)-1])
	}
	p.logger.Infof("start unfreezing server from dump %s", serverDumpID)
	path := dump.NewServerDumpPath(p.directory, p.project, serverName, serverDumpID)
	_, err := os.Stat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("could not stat dir %s: %w", path, err)
	}
	serverDump, err := dump.LoadServer(path)
	if err != nil {
		return fmt.Errorf("failed to load server dump: %w", err)
	}
	p.logger.Infof("create cloud init config for server")
	var floatingIPs []*hcloud.FloatingIP
	for _, fIP := range serverDump.FloatingIPs {
		fIP, resp, err := p.client.FloatingIP.GetByID(ctx, fIP.ID)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode > 201 {
			return fmt.Errorf("could not get floating ip: status %d ", resp.StatusCode)
		}
		floatingIPs = append(floatingIPs, fIP)
	}
	floatingIPsStrs := lo.Map(floatingIPs, func(item *hcloud.FloatingIP, index int) string { return fmt.Sprintf("%s/32", item.IP.String()) })
	var modifiedContent string
	if len(floatingIPsStrs) > 0 {
		modifiedContent = strings.ReplaceAll(cloudInitContent, "0.0.0.0/32", strings.Join(floatingIPsStrs, " "))
		modifiedContent = strings.Join(strings.Split(modifiedContent, " "), ", ")
		modifiedContent = fmt.Sprintf("#cloud-config\nruncmd:\n- [%s]", modifiedContent)
	}

	p.logger.Infof("create server from dump")
	publicNet := &hcloud.ServerCreatePublicNet{}
	if serverDump.Server.PublicNet.IPv4.ID != 0 {
		// TODO check if ipv4 is blocked
		//publicNet.EnableIPv4 = !serverDump.Server.PublicNet.IPv4.Blocked
		publicNet.EnableIPv4 = true
		publicNet.IPv4 = &hcloud.PrimaryIP{
			ID: serverDump.Server.PublicNet.IPv4.ID,
		}
	}
	if serverDump.Server.PublicNet.IPv6.ID != 0 {
		// TODO check if ipv6 is blocked
		//publicNet.EnableIPv6 = !serverDump.Server.PublicNet.IPv6.Blocked
		publicNet.EnableIPv6 = true
		publicNet.IPv6 = &hcloud.PrimaryIP{
			ID: serverDump.Server.PublicNet.IPv6.ID,
		}
	}
	firewalls := lo.Map(serverDump.Server.PublicNet.Firewalls, func(item schema.ServerFirewall, index int) *hcloud.ServerCreateFirewall {
		return &hcloud.ServerCreateFirewall{Firewall: hcloud.Firewall{ID: item.ID}}
	})
	volumes := lo.Map(serverDump.Server.Volumes, func(item int64, index int) *hcloud.Volume { return &hcloud.Volume{ID: item} })
	sshKeys := lo.Map(serverDump.SSHKeys, func(item schema.SSHKey, index int) *hcloud.SSHKey {
		return &hcloud.SSHKey{ID: item.ID}
	})
	var placementGroup *hcloud.PlacementGroup
	if serverDump.Server.PlacementGroup != nil {
		placementGroup = &hcloud.PlacementGroup{ID: serverDump.Server.PlacementGroup.ID}
	}
	createRes, resp, err := p.client.Server.Create(ctx, hcloud.ServerCreateOpts{
		Name:           serverDump.Server.Name,
		ServerType:     &hcloud.ServerType{ID: serverDump.Server.ServerType.ID},
		Image:          &hcloud.Image{ID: serverDump.Snapshot.ID},
		SSHKeys:        sshKeys,
		Datacenter:     &hcloud.Datacenter{ID: serverDump.Server.Datacenter.ID},
		UserData:       modifiedContent,
		Labels:         serverDump.Server.Labels,
		Volumes:        volumes,
		Firewalls:      firewalls,
		PlacementGroup: placementGroup,
		PublicNet:      publicNet,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 201 {
		return fmt.Errorf("could not create server: status %d ", resp.StatusCode)
	}
	err = p.waitForActionStatus(ctx, createRes.Action)
	if err != nil {
		return err
	}

	for _, fIP := range floatingIPs {
		p.logger.Infof("assign floating ip %s to server", fIP.IP.String())
		action, resp, err := p.client.FloatingIP.Assign(ctx, fIP, createRes.Server)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode > 201 {
			return fmt.Errorf("could not get floating ip: status %d ", resp.StatusCode)
		}
		err = p.waitForActionStatus(ctx, action)
		if err != nil {
			return err
		}
	}
	for _, pNet := range serverDump.Server.PrivateNet {
		p.logger.Infof("assign private network ip %s to server", pNet.IP)
		ipParts := strings.Split(pNet.IP, ".")
		if len(ipParts) != 4 {
			continue
		}
		ipPartsInts := lo.Map(ipParts, func(item string, index int) int {
			num, _ := strconv.Atoi(item)
			return num
		})
		action, resp, err := p.client.Server.AttachToNetwork(ctx,
			createRes.Server,
			hcloud.ServerAttachToNetworkOpts{Network: &hcloud.Network{ID: pNet.Network},
				IP: net.IPv4(byte(ipPartsInts[0]), byte(ipPartsInts[1]), byte(ipPartsInts[2]), byte(ipPartsInts[3]))})
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode > 201 {
			return fmt.Errorf("could not attach server to private network: status %d ", resp.StatusCode)
		}
		err = p.waitForActionStatus(ctx, action)
		if err != nil {
			return err
		}
	}
	p.logger.Infof("finish unfreezing server")
	return nil
}

func (p *resolverService) FreezeServer(ctx context.Context, serverName string) (string, error) {
	p.logger.Infof("start freezing server %s", serverName)
	newID := strconv.Itoa(time.Now().UTC().Nanosecond())
	svr, resp, err := p.client.Server.GetByName(ctx, serverName)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 201 {
		return "", fmt.Errorf("could not get server by name: status %d ", resp.StatusCode)
	}
	if svr == nil {
		return "", fmt.Errorf("server with name %s not found", serverName)
	}
	p.logger.Infof("shutdown server %s", serverName)
	action, resp, err := p.client.Server.Shutdown(ctx, svr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 201 {
		return "", fmt.Errorf("could not shutdown server: status %d ", resp.StatusCode)
	}
	err = p.waitForActionStatus(ctx, action)
	if err != nil {
		return "", err
	}
	p.logger.Infof("create dump of server %s", serverName)
	serverDump, err := p.createServerDump(ctx, newID, svr)
	if err != nil {
		return "", err
	}
	if len(serverDump.FloatingIPs) > 0 {
		p.logger.Infof("unassign floating ips of server %s", serverName)
		for _, fIP := range serverDump.FloatingIPs {
			assignedfIP, resp, err := p.client.FloatingIP.GetByID(ctx, fIP.ID)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			if resp.StatusCode > 201 {
				return "", fmt.Errorf("could not get floating ip: status %d ", resp.StatusCode)
			}
			action, resp, err = p.client.FloatingIP.Unassign(ctx, assignedfIP)
			if err != nil {
				return "", err
			}
			defer resp.Body.Close()
			if resp.StatusCode > 201 {
				return "", fmt.Errorf("could not unassign floating ip: status %d ", resp.StatusCode)
			}
			err = p.waitForActionStatus(ctx, action)
			if err != nil {
				return "", err
			}
		}
	}
	if serverDump.Server.PublicNet.IPv4.ID != 0 {
		p.logger.Infof("unassign ipv4 of server %s", serverName)
		action, resp, err = p.client.PrimaryIP.Unassign(ctx, serverDump.Server.PublicNet.IPv4.ID)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode > 201 {
			return "", fmt.Errorf("could not unassign ipv4: status %d ", resp.StatusCode)
		}
		err = p.waitForActionStatus(ctx, action)
		if err != nil {
			return "", err
		}
	}
	if serverDump.Server.PublicNet.IPv6.ID != 0 {
		p.logger.Infof("unassign ipv6 of server %s", serverName)
		action, resp, err = p.client.PrimaryIP.Unassign(ctx, serverDump.Server.PublicNet.IPv6.ID)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode > 201 {
			return "", fmt.Errorf("could not unassign ipv6: status %d ", resp.StatusCode)
		}
		err = p.waitForActionStatus(ctx, action)
		if err != nil {
			return "", err
		}
	}
	p.logger.Infof("delete server %s", serverName)
	deleteRes, resp, err := p.client.Server.DeleteWithResult(ctx, svr)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 201 {
		return "", fmt.Errorf("could not delete server: status %s ", resp.StatusCode)
	}
	err = p.waitForActionStatus(ctx, deleteRes.Action)
	if err != nil {
		return "", err
	}
	p.logger.Infof("finish freezing server %s", serverName)
	return newID, nil
}

func (p *resolverService) waitForActionStatus(ctx context.Context, action *hcloud.Action) error {
	if action.Status == hcloud.ActionStatusSuccess {
		p.logger.Infof("action progress 100/100")
		return nil
	}
	ticker := time.NewTicker(pollingInterval)
	deadline := time.After(pollingDeadline)
	var statusAction *hcloud.Action
	for {
		select {
		case <-ticker.C:
			var resp *hcloud.Response
			var err error
			statusAction, resp, err = p.client.Action.GetByID(ctx, action.ID)
			if err != nil {
				return err
			}
			if resp.StatusCode > 201 {
				return fmt.Errorf("could not get action status: status %d ", resp.StatusCode)
			}
			defer resp.Body.Close()
			if statusAction.Status == hcloud.ActionStatusSuccess {
				p.logger.Infof("action progress 100/100")
				return nil
			} else if statusAction.Status == hcloud.ActionStatusError {
				if strings.Contains(statusAction.ErrorMessage, "Unknown error") {
					continue
				}
				return fmt.Errorf("action status %s: message: %s", statusAction.Status, statusAction.ErrorMessage)
			}
			p.logger.Infof("action progress %d/100", statusAction.Progress)
		case <-deadline:
			return fmt.Errorf("wait for action deadline reached, action status %s", statusAction.Status)
		case <-ctx.Done():
			return fmt.Errorf("wait for action context was cancelled, action status %s", statusAction.Status)
		}
	}
}

func (p *resolverService) CreateServerDump(ctx context.Context, serverName string) (string, error) {
	newID := strconv.Itoa(time.Now().UTC().Nanosecond())
	svr, resp, err := p.client.Server.GetByName(ctx, serverName)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode > 201 {
		return "", fmt.Errorf("could not get server by name: status %d ", resp.StatusCode)
	}
	_, err = p.createServerDump(ctx, newID, svr)
	if err != nil {
		return "", err
	}
	return newID, nil
}

func (p *resolverService) createServerDump(ctx context.Context, serverDumpID string, svr *hcloud.Server) (*dump.ServerDump, error) {
	fIPs, resp, err := p.client.FloatingIP.List(ctx, hcloud.FloatingIPListOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to list floating ips: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode > 201 {
		return nil, fmt.Errorf("could not list floating ips: status %d ", resp.StatusCode)
	}
	assignedFIPs := lo.Filter(fIPs, func(fIP *hcloud.FloatingIP, _ int) bool {
		if fIP.Server == nil {
			return false
		}
		return fIP.Server.ID == svr.ID
	})
	sshKeys, resp, err := p.client.SSHKey.List(ctx, hcloud.SSHKeyListOpts{})
	if err != nil {
		return nil, fmt.Errorf("failed to list ssh keys: %w", err)
	}
	if resp.StatusCode > 201 {
		return nil, fmt.Errorf("could not list ssh keys: status %d ", resp.StatusCode)
	}
	defer resp.Body.Close()
	description := time.Now().Format("2006-01-02 15:04:05")
	p.logger.Infof("create snapshot of server %d", svr.ID)
	srvImg, resp, err := p.client.Server.CreateImage(ctx, svr, &hcloud.ServerCreateImageOpts{
		Description: &description,
		Type:        hcloud.ImageTypeSnapshot,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create image: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode > 201 {
		return nil, fmt.Errorf("could not create image: status %d ", resp.StatusCode)
	}
	err = p.waitForActionStatus(ctx, srvImg.Action)
	if err != nil {
		return nil, err
	}

	schSrv := hcloud.SchemaFromServer(svr)
	schFIPs := lo.Map(assignedFIPs, func(item *hcloud.FloatingIP, index int) schema.FloatingIP {
		return hcloud.SchemaFromFloatingIP(item)
	})
	schSSHKeys := lo.Map(sshKeys, func(item *hcloud.SSHKey, index int) schema.SSHKey {
		return hcloud.SchemaFromSSHKey(item)
	})
	path := dump.NewServerDumpPath(p.directory, p.project, svr.Name, serverDumpID)
	path, err = dump.EnsureHasDirectory(path)
	if err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}
	schSnapshot := hcloud.SchemaFromImage(srvImg.Image)
	serverDump := &dump.ServerDump{
		Server:      schSrv,
		FloatingIPs: schFIPs,
		SSHKeys:     schSSHKeys,
		Snapshot:    schSnapshot,
	}
	err = dump.StoreServer(path, serverDump)
	if err != nil {
		return nil, err
	}
	return serverDump, nil
}

func GetDirectoriesNames(dirPath string) ([]string, error) {
	var directories []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			directories = append(directories, info.Name())
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return directories, nil
}

func NewProvider(logger *logrus.Logger, project string, hcli *hcloud.Client) Resolver {
	return &resolverService{
		logger:  logger,
		project: project,
		client:  hcli,
	}
}
