package main

import (
	"context"
	"os"
	"time"

	"github.com/namsral/flag"

	"github.com/govirtuo/kube-ns-suspender/engine"
	"github.com/govirtuo/kube-ns-suspender/metrics"
	"github.com/govirtuo/kube-ns-suspender/webui"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
)

func main() {
	var opt engine.Options
	var err error
	fs := flag.NewFlagSetWithEnvPrefix(os.Args[0], "KUBE_NS_SUSPENDER", 0)
	fs.StringVar(&opt.LogLevel, "loglevel", "debug", "Log level")
	fs.StringVar(&opt.TZ, "timezone", "Europe/Paris", "Timezone to use")
	fs.IntVar(&opt.WatcherIdle, "watcher-idle", 15, "Watcher idle duration (in seconds)")
	fs.BoolVar(&opt.DryRun, "dry-run", false, "Run in dry run mode")
	fs.BoolVar(&opt.NoKubeWarnings, "no-kube-warnings", false, "Disable Kubernetes warnings")
	fs.BoolVar(&opt.WebUI, "web-ui", true, "Start web UI on port 8080")

	if err := fs.Parse(os.Args[1:]); err != nil {
		log.Fatal().Err(err).Msg("cannot parse flags")
	}

	// set the local timezone
	time.Local, err = time.LoadLocation(opt.TZ)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot load timezone")
	}

	// create the engine
	eng, err := engine.New(opt)
	if err != nil {
		log.Fatal().Err(err).Msg("cannot create new engine")
	}
	eng.Logger.Debug().Msgf("timezone: %s", time.Local.String())
	eng.Logger.Debug().Msgf("watcher idle: %s", time.Duration(eng.Options.WatcherIdle)*time.Second)
	eng.Logger.Debug().Msgf("log level: %s", eng.Options.LogLevel)
	eng.Logger.Info().Msg("kube-ns-suspender launched")

	// start web ui
	if eng.Options.WebUI {
		go func() {
			uiLogger := eng.Logger.With().
				Str("routine", "webui").Logger()
			if err := webui.Start(uiLogger, "8080"); err != nil {
				uiLogger.Fatal().Err(err).Msg("web UI failed")
			}
		}()
		eng.Logger.Info().Msg("web UI successfully created")
	}

	// create metrics server
	eng.MetricsServ = *metrics.Init()

	// start metrics server
	go func() {
		if err := eng.MetricsServ.Start(); err != nil {
			eng.Logger.Fatal().Err(err).Msg("metrics server failed")
		}
	}()
	eng.Logger.Info().Msg("metrics server successfully created")

	// create the in-cluster config
	config, err := rest.InClusterConfig()
	if err != nil {
		eng.Logger.Fatal().Err(err).Msg("cannot create in-cluster configuration")
	}
	eng.Logger.Info().Msg("in-cluster configuration successfully created")

	// disable k8s warnings
	if eng.Options.NoKubeWarnings {
		config.WarningHandler = rest.NoWarnings{}
	}

	// create the clientset
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		eng.Logger.Fatal().Err(err).Msg("cannot create the clientset")
	}
	eng.Logger.Info().Msg("clientset successfully created")

	ctx := context.Background()
	go eng.Watcher(ctx, clientset)
	go eng.Suspender(ctx, clientset)

	// wait forever
	select {}
}
