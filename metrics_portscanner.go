package main

import (
	"context"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/network/mgmt/network"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	"os"
)

type MetricsCollectorPortscanner struct {
	CollectorProcessorCustom

	portscanner *Portscanner

	prometheus struct {
		publicIpPortscanStatus  *prometheus.GaugeVec
		publicIpPortscanUpdated *prometheus.GaugeVec
		publicIpPortscanPort    *prometheus.GaugeVec
	}
}

func (m *MetricsCollectorPortscanner) Setup(collector *CollectorCustom) {
	m.CollectorReference = collector

	m.portscanner = &Portscanner{}
	m.portscanner.Init()

	m.prometheus.publicIpPortscanStatus = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurerm_publicip_portscan_status",
			Help: "Azure ResourceManager public ip portscan status",
		},
		[]string{
			"ipAddress",
			"type",
		},
	)

	m.prometheus.publicIpPortscanPort = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurerm_publicip_portscan_port",
			Help: "Azure ResourceManager public ip port",
		},
		[]string{
			"ipAddress",
			"protocol",
			"port",
			"description",
		},
	)

	prometheus.MustRegister(m.prometheus.publicIpPortscanStatus)
	prometheus.MustRegister(m.prometheus.publicIpPortscanPort)

	m.portscanner.Callbacks.FinishScan = func(c *Portscanner) {
		m.logger().Infof("finished for %v IPs", len(m.portscanner.PublicIps))

		if opts.Cache.Path != "" {
			m.logger().Infof("saved to cache")
			m.portscanner.CacheSave(opts.Cache.Path)
		}
	}

	m.portscanner.Callbacks.StartupScan = func(c *Portscanner) {
		m.logger().Infof(
			"starting for %v IPs (parallel:%v, threads per run:%v, timeout:%vs, portranges:%v)",
			len(c.PublicIps),
			opts.Portscan.Prallel,
			opts.Portscan.Threads,
			opts.Portscan.Timeout,
			portscanPortRange,
		)

		m.prometheus.publicIpPortscanStatus.Reset()
	}

	m.portscanner.Callbacks.StartScanIpAdress = func(c *Portscanner, ipAddress string) {
		m.logger().WithField("ipAddress", ipAddress).Infof("start port scanning")

		// set the ipAdress to be scanned
		m.prometheus.publicIpPortscanStatus.With(prometheus.Labels{
			"ipAddress": ipAddress,
			"type":      "finished",
		}).Set(0)
	}

	m.portscanner.Callbacks.FinishScanIpAdress = func(c *Portscanner, ipAddress string, elapsed float64) {
		// set ipAddess to be finsihed
		m.prometheus.publicIpPortscanStatus.With(prometheus.Labels{
			"ipAddress": ipAddress,
			"type":      "finished",
		}).Set(1)

		// set the elapsed time
		m.prometheus.publicIpPortscanStatus.With(prometheus.Labels{
			"ipAddress": ipAddress,
			"type":      "elapsed",
		}).Set(elapsed)

		// set update time
		m.prometheus.publicIpPortscanStatus.With(prometheus.Labels{
			"ipAddress": ipAddress,
			"type":      "updated",
		}).SetToCurrentTime()
	}

	m.portscanner.Callbacks.ResultCleanup = func(c *Portscanner) {
		m.prometheus.publicIpPortscanPort.Reset()
	}

	m.portscanner.Callbacks.ResultPush = func(c *Portscanner, result PortscannerResult) {
		m.prometheus.publicIpPortscanPort.With(result.Labels).Set(result.Value)
	}

	if opts.Cache.Path != "" {
		if _, err := os.Stat(opts.Cache.Path); !os.IsNotExist(err) {
			m.logger().Infof("load from cache")
			m.portscanner.CacheLoad(opts.Cache.Path)
		}
	}
}

func (m *MetricsCollectorPortscanner) Collect(ctx context.Context, logger *log.Entry) {
	ipAdressList := m.fetchPublicIpAdresses(ctx, logger, m.CollectorReference.AzureSubscriptions)

	m.portscanner.SetIps(ipAdressList)

	if len(ipAdressList) > 0 {
		m.portscanner.Start()
	}
}

func (m *MetricsCollectorPortscanner) fetchPublicIpAdresses(ctx context.Context, logger *log.Entry, subscriptions []subscriptions.Subscription) (ipAddressList []string) {
	logger.Info("collecting public ips")

	for _, subscription := range subscriptions {
		contextLogger := logger.WithField("azureSubscription", subscription)

		client := network.NewPublicIPAddressesClient(*subscription.SubscriptionID)
		client.Authorizer = AzureAuthorizer

		list, err := client.ListAll(ctx)
		if err != nil {
			contextLogger.Panic(err)
		}

		for _, val := range list.Values() {
			if val.IPAddress != nil {
				ipAddressList = append(ipAddressList, *val.IPAddress)
			}
		}
	}

	return ipAddressList
}
