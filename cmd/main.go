package main

import (
	"context"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"hetzner-freezer/resolver"
	"sigs.k8s.io/controller-runtime/pkg/manager/signals"
	"time"
)

const backOffDuration = 5 * time.Second
const pollBackOffDuration = 5 * time.Second

func main() {
	ctx := signals.SetupSignalHandler()
	logger := logrus.New()

	root := cobra.Command{}
	root.AddCommand(
		NewFreezeCommand(ctx, logger),
		NewUnfreezeCommand(ctx, logger),
		NewServerDumpCommand(ctx, logger),
	)
	if err := root.Execute(); err != nil {
		logger.Fatal(err)
		logrus.Exit(1)

	}
	logrus.Exit(0)
}

func NewFreezeCommand(ctx context.Context, log *logrus.Logger) *cobra.Command {
	var serverName string
	var project string
	var token string
	cmd := &cobra.Command{
		Use:   "freeze",
		Short: "Run hetzner freezer",
		Run: func(cmd *cobra.Command, args []string) {

			client := hcloud.NewClient(hcloud.WithToken(token),
				hcloud.WithBackoffFunc(func(_ int) time.Duration { return backOffDuration }),
				hcloud.WithPollBackoffFunc(func(r int) time.Duration { return pollBackOffDuration }))

			p := resolver.NewProvider(log, project, client)

			fingerPrintID, err := p.FreezeServer(ctx, serverName)
			if err != nil {
				log.Errorf("could not freeze server: %v", err)
				return
			}
			log.Info(fingerPrintID)
		},
	}
	cmd.PersistentFlags().StringVar(&serverName, "server-name", "", "hetzner server name")
	cmd.PersistentFlags().StringVar(&project, "project", "", "hetzner project name")
	cmd.PersistentFlags().StringVar(&token, "token", "", "hetzner API token")
	if err := cmd.MarkPersistentFlagRequired("server-name"); err != nil {
		log.Fatal(err)
	} else if err := cmd.MarkPersistentFlagRequired("project"); err != nil {
		log.Fatal(err)
	} else if err := cmd.MarkPersistentFlagRequired("token"); err != nil {
		log.Fatal(err)
	}
	return cmd
}

func NewUnfreezeCommand(ctx context.Context, log *logrus.Logger) *cobra.Command {
	var serverName string
	var serverDumpID string
	var project string
	var token string
	cmd := &cobra.Command{
		Use:   "unfreeze",
		Short: "Run hetzner freezer",
		Run: func(cmd *cobra.Command, args []string) {

			client := hcloud.NewClient(hcloud.WithToken(token),
				hcloud.WithBackoffFunc(func(_ int) time.Duration { return backOffDuration }),
				hcloud.WithPollBackoffFunc(func(r int) time.Duration { return pollBackOffDuration }))

			p := resolver.NewProvider(log, project, client)

			err := p.UnfreezeServer(ctx, serverName, serverDumpID)
			if err != nil {
				log.Errorf("could not unfreeze server: %v", err)
				return
			}
		},
	}
	cmd.PersistentFlags().StringVar(&serverName, "server-name", "", "hetzner server name")
	cmd.PersistentFlags().StringVar(&serverDumpID, "server-dump-id", "", "hetzner server dump id")
	cmd.PersistentFlags().StringVar(&project, "project", "", "hetzner project name")
	cmd.PersistentFlags().StringVar(&token, "token", "", "hetzner API token")
	if err := cmd.MarkPersistentFlagRequired("server-name"); err != nil {
		log.Fatal(err)
	} else if err := cmd.MarkPersistentFlagRequired("project"); err != nil {
		log.Fatal(err)
	} else if err := cmd.MarkPersistentFlagRequired("token"); err != nil {
		log.Fatal(err)
	}
	return cmd
}

func NewServerDumpCommand(ctx context.Context, log *logrus.Logger) *cobra.Command {
	var serverName string
	var project string
	var token string
	cmd := &cobra.Command{
		Use:   "dump",
		Short: "Run hetzner server dump",
		Run: func(cmd *cobra.Command, args []string) {

			client := hcloud.NewClient(hcloud.WithToken(token),
				hcloud.WithBackoffFunc(func(_ int) time.Duration { return backOffDuration }),
				hcloud.WithPollBackoffFunc(func(r int) time.Duration { return pollBackOffDuration }))

			p := resolver.NewProvider(log, project, client)

			fingerPrintID, err := p.CreateServerDump(ctx, serverName)
			if err != nil {
				log.Errorf("could not freeze server: %v", err)
				return
			}
			log.Info(fingerPrintID)
		},
	}
	cmd.PersistentFlags().StringVar(&serverName, "server-name", "", "hetzner server name")
	cmd.PersistentFlags().StringVar(&project, "project", "", "hetzner project name")
	cmd.PersistentFlags().StringVar(&token, "token", "", "hetzner API token")
	if err := cmd.MarkPersistentFlagRequired("server-name"); err != nil {
		log.Fatal(err)
	} else if err := cmd.MarkPersistentFlagRequired("project"); err != nil {
		log.Fatal(err)
	} else if err := cmd.MarkPersistentFlagRequired("token"); err != nil {
		log.Fatal(err)
	}
	return cmd
}
