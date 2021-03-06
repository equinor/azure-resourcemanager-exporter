package main

import (
	"context"
	"fmt"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/advisor/mgmt/advisor"
	"github.com/Azure/azure-sdk-for-go/profiles/latest/resources/mgmt/subscriptions"
	"github.com/Azure/azure-sdk-for-go/profiles/preview/preview/security/mgmt/security"
	"github.com/prometheus/client_golang/prometheus"
	log "github.com/sirupsen/logrus"
	prometheusCommon "github.com/webdevops/go-prometheus-common"
	"time"
)

type MetricsCollectorAzureRmSecurity struct {
	CollectorProcessorGeneral

	prometheus struct {
		securitycenterCompliance *prometheus.GaugeVec
		advisorRecommendations   *prometheus.GaugeVec
	}
}

func (m *MetricsCollectorAzureRmSecurity) Setup(collector *CollectorGeneral) {
	m.CollectorReference = collector

	m.prometheus.securitycenterCompliance = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurerm_securitycenter_compliance",
			Help: "Azure Audit SecurityCenter compliance status",
		},
		[]string{
			"subscriptionID",
			"assessmentType",
		},
	)

	m.prometheus.advisorRecommendations = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "azurerm_advisor_recommendation",
			Help: "Azure Audit Advisor recommendation",
		},
		[]string{
			"subscriptionID",
			"category",
			"resourceType",
			"resourceName",
			"resourceGroup",
			"problem",
			"impact",
			"risk",
		},
	)

	prometheus.MustRegister(m.prometheus.securitycenterCompliance)
	prometheus.MustRegister(m.prometheus.advisorRecommendations)
}

func (m *MetricsCollectorAzureRmSecurity) Reset() {
	m.prometheus.securitycenterCompliance.Reset()
	m.prometheus.advisorRecommendations.Reset()
}

func (m *MetricsCollectorAzureRmSecurity) Collect(ctx context.Context, logger *log.Entry, callback chan<- func(), subscription subscriptions.Subscription) {
	m.collectAzureAdvisorRecommendations(ctx, logger, callback, subscription)
	for _, location := range m.CollectorReference.AzureLocations {
		m.collectAzureSecurityCompliance(ctx, logger, callback, subscription, location)
	}
}

func (m *MetricsCollectorAzureRmSecurity) collectAzureSecurityCompliance(ctx context.Context, logger *log.Entry, callback chan<- func(), subscription subscriptions.Subscription, location string) {
	subscriptionResourceId := fmt.Sprintf("/subscriptions/%v", *subscription.SubscriptionID)
	client := security.NewCompliancesClient(subscriptionResourceId, location)
	client.Authorizer = AzureAuthorizer

	complienceResult, err := client.Get(ctx, subscriptionResourceId, time.Now().Format("2006-01-02Z"))
	if err != nil {
		logger.Error(err)
		return
	}

	infoMetric := prometheusCommon.NewMetricsList()

	if complienceResult.AssessmentResult != nil {
		for _, result := range *complienceResult.AssessmentResult {
			segmentType := ""
			if result.SegmentType != nil {
				segmentType = *result.SegmentType
			}

			infoLabels := prometheus.Labels{
				"subscriptionID": *subscription.SubscriptionID,
				"assessmentType": segmentType,
			}
			infoMetric.Add(infoLabels, *result.Percentage)
		}
	}

	callback <- func() {
		infoMetric.GaugeSet(m.prometheus.securitycenterCompliance)
	}
}

func (m *MetricsCollectorAzureRmSecurity) collectAzureAdvisorRecommendations(ctx context.Context, logger *log.Entry, callback chan<- func(), subscription subscriptions.Subscription) {
	client := advisor.NewRecommendationsClient(*subscription.SubscriptionID)
	client.Authorizer = AzureAuthorizer

	recommendationResult, err := client.ListComplete(ctx, "", nil, "")
	if err != nil {
		logger.Panic(err)
	}

	infoMetric := prometheusCommon.NewMetricsList()

	for _, item := range *recommendationResult.Response().Value {

		infoLabels := prometheus.Labels{
			"subscriptionID": *subscription.SubscriptionID,
			"category":       string(item.RecommendationProperties.Category),
			"resourceType":   stringPtrToString(item.RecommendationProperties.ImpactedField),
			"resourceName":   stringPtrToString(item.RecommendationProperties.ImpactedValue),
			"resourceGroup":  extractResourceGroupFromAzureId(*item.ID),
			"problem":        stringPtrToString(item.RecommendationProperties.ShortDescription.Problem),
			"impact":         string(item.Impact),
			"risk":           string(item.Risk),
		}

		infoMetric.AddInfo(infoLabels)
	}

	callback <- func() {
		infoMetric.GaugeSet(m.prometheus.advisorRecommendations)
	}
}
