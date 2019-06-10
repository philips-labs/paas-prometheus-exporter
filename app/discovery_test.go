package app_test

import (
	"context"
	"errors"
	"time"

	"github.com/alphagov/paas-prometheus-exporter/test"
	sonde_events "github.com/cloudfoundry/sonde-go/events"

	dto "github.com/prometheus/client_model/go"

	"github.com/alphagov/paas-prometheus-exporter/app"
	"github.com/alphagov/paas-prometheus-exporter/cf"
	"github.com/alphagov/paas-prometheus-exporter/cf/mocks"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cloudfoundry-community/go-cfclient"
)

const guid = "33333333-3333-3333-3333-333333333333"

var appFixture = cf.AppWithProcesses{
	AppGUID:   guid,
	Processes: []cfclient.Process{},
	App: cfclient.App{
		Guid:      guid,
		Instances: 1,
		Name:      "foo",
		State:     "STARTED",
		SpaceData: cfclient.SpaceResource{
			Entity: cfclient.Space{
				Name: "spacename",
				OrgData: cfclient.OrgResource{
					Entity: cfclient.Org{Name: "orgname"},
				},
			},
		},
	},
}

var _ = Describe("CheckForNewApps", func() {

	var discovery *app.Discovery
	var fakeClient *mocks.FakeClient
	var ctx context.Context
	var cancel context.CancelFunc
	var registry *prometheus.Registry
	var fakeAppStreamProvider *mocks.FakeAppStreamProvider
	var errChan chan error

	BeforeEach(func() {
		fakeClient = &mocks.FakeClient{}
		fakeAppStreamProvider = &mocks.FakeAppStreamProvider{}
		fakeClient.NewAppStreamProviderReturns(fakeAppStreamProvider)
		registry = prometheus.NewRegistry()
		discovery = app.NewDiscovery(fakeClient, registry, 100*time.Millisecond)
		ctx, cancel = context.WithCancel(context.Background())
		errChan = make(chan error, 1)
	})

	AfterEach(func() {
		cancel()
	})

	It("checks for new process regularly", func() {
		discovery.Start(ctx, errChan)

		Eventually(fakeClient.ListProcessWithAppsSpaceAndOrgCallCount).Should(Equal(2))
	})

	It("returns an error if it fails to list the apps", func() {
		err := errors.New("some error")
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns(nil, err)

		discovery.Start(ctx, errChan)

		Eventually(errChan).Should(Receive(MatchError(err)))

		Consistently(fakeClient.ListProcessWithAppsSpaceAndOrgCallCount, 200*time.Millisecond).Should(Equal(1))
	})

	It("creates a new app", func() {
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns([]cf.AppWithProcesses{appFixture}, nil)

		discovery.Start(ctx, errChan)

		Eventually(fakeClient.NewAppStreamProviderCallCount).Should(Equal(1))

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid": guid,
			})
		}).ShouldNot(BeNil())
	})

	It("does not create a new appWatcher for each process if the app state is stopped", func() {
		stoppedApp := appFixture
		stoppedApp.App.State = "STOPPED"
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns([]cf.AppWithProcesses{stoppedApp}, nil)

		discovery.Start(ctx, errChan)

		Consistently(fakeClient.NewAppStreamProviderCallCount, 200*time.Millisecond).Should(Equal(0))

		Consistently(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid": guid,
			})
		}, 200*time.Millisecond).Should(BeNil())
	})

	XIt("deletes an AppWatcher when an app is deleted", func() {
		fakeClient.ListProcessWithAppsSpaceAndOrgReturnsOnCall(0, []cf.AppWithProcesses{appFixture}, nil)
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns([]cf.AppWithProcesses{}, nil)

		discovery.Start(ctx, errChan)

		Eventually(fakeClient.NewAppStreamProviderCallCount).Should(Equal(1))
		Eventually(func() []*dto.Metric { return test.GetMetrics(registry) }).ShouldNot(BeEmpty())

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid": guid,
			})
		}).Should(BeNil())
	})

	XIt("deletes an AppWatcher when an app is stopped", func() {
		fakeClient.ListProcessWithAppsSpaceAndOrgReturnsOnCall(0, []cf.AppWithProcesses{appFixture}, nil)

		stoppedApp := appFixture
		stoppedApp.App.State = "STOPPED"
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns([]cf.AppWithProcesses{stoppedApp}, nil)

		discovery.Start(ctx, errChan)

		Eventually(fakeClient.NewAppStreamProviderCallCount).Should(Equal(1))
		Eventually(func() []*dto.Metric { return test.GetMetrics(registry) }).ShouldNot(BeEmpty())

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid": guid,
			})
		}).Should(BeNil())
	})

	XIt("deletes and recreates an AppWatcher when an app is renamed", func() {
		app1 := appFixture
		app1.App.Name = "foo"
		fakeClient.ListProcessWithAppsSpaceAndOrgReturnsOnCall(0, []cf.AppWithProcesses{app1}, nil)

		app2 := appFixture
		app2.App.Name = "bar"
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns([]cf.AppWithProcesses{app2}, nil)

		fakeAppStreamProvider1 := &mocks.FakeAppStreamProvider{}
		fakeClient.NewAppStreamProviderReturnsOnCall(0, fakeAppStreamProvider1)
		fakeAppStreamProvider2 := &mocks.FakeAppStreamProvider{}
		fakeClient.NewAppStreamProviderReturnsOnCall(1, fakeAppStreamProvider2)

		discovery.Start(ctx, errChan)

		Eventually(fakeClient.NewAppStreamProviderCallCount).Should(Equal(2))

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid": guid,
				"app":  "bar",
			})
		}).ShouldNot(BeNil())

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid": guid,
				"app":  "foo",
			})
		}).Should(BeNil())
	})

	XIt("deletes and recreates an AppWatcher when an app's space is renamed", func() {
		app1 := appFixture
		app1.App.SpaceData.Entity.Name = "spacename"
		fakeClient.ListProcessWithAppsSpaceAndOrgReturnsOnCall(0, []cf.AppWithProcesses{app1}, nil)

		app2 := appFixture
		app2.App.SpaceData.Entity.Name = "spacenamenew"
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns([]cf.AppWithProcesses{app2}, nil)

		fakeAppStreamProvider1 := &mocks.FakeAppStreamProvider{}
		fakeClient.NewAppStreamProviderReturnsOnCall(0, fakeAppStreamProvider1)
		fakeAppStreamProvider2 := &mocks.FakeAppStreamProvider{}
		fakeClient.NewAppStreamProviderReturnsOnCall(1, fakeAppStreamProvider2)

		discovery.Start(ctx, errChan)

		Eventually(fakeClient.NewAppStreamProviderCallCount).Should(Equal(2))

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid":  guid,
				"space": "spacenamenew",
			})
		}).ShouldNot(BeNil())

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid":  guid,
				"space": "spacename",
			})
		}).Should(BeNil())
	})

	XIt("deletes and recreates an AppWatcher when an app's org is renamed", func() {
		app1 := appFixture
		app1.App.SpaceData.Entity.OrgData.Entity.Name = "orgname"
		fakeClient.ListProcessWithAppsSpaceAndOrgReturnsOnCall(0, []cf.AppWithProcesses{app1}, nil)

		app2 := appFixture
		app2.App.SpaceData.Entity.OrgData.Entity.Name = "orgnamenew"
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns([]cf.AppWithProcesses{app2}, nil)

		fakeAppStreamProvider1 := &mocks.FakeAppStreamProvider{}
		fakeClient.NewAppStreamProviderReturnsOnCall(0, fakeAppStreamProvider1)
		fakeAppStreamProvider2 := &mocks.FakeAppStreamProvider{}
		fakeClient.NewAppStreamProviderReturnsOnCall(1, fakeAppStreamProvider2)

		discovery.Start(ctx, errChan)

		Eventually(fakeClient.NewAppStreamProviderCallCount).Should(Equal(2))

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid":         guid,
				"organisation": "orgnamenew",
			})
		}).ShouldNot(BeNil())

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid":         guid,
				"organisation": "orgname",
			})
		}).Should(BeNil())
	})

	XIt("updates an AppWatcher when an app changes size", func() {
		fakeClient.ListProcessWithAppsSpaceAndOrgReturnsOnCall(0, []cf.AppWithProcesses{appFixture}, nil)

		appWithTwoInstances := appFixture
		appWithTwoInstances.App.Instances = 2
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns([]cf.AppWithProcesses{appWithTwoInstances}, nil)

		discovery.Start(ctx, errChan)

		Eventually(fakeClient.NewAppStreamProviderCallCount).Should(Equal(1))

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid":     guid,
				"instance": "0",
			})
		}).ShouldNot(BeNil())

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid":     guid,
				"instance": "1",
			})
		}).ShouldNot(BeNil())
	})

	XIt("recreates an AppWatcher when it has an error", func() {
		fakeClient.ListProcessWithAppsSpaceAndOrgReturns([]cf.AppWithProcesses{appFixture}, nil)
		appEnvelopChan1 := make(chan *sonde_events.Envelope, 1)
		close(appEnvelopChan1)
		errChan := make(chan error, 1)

		fakeAppStreamProvider.StartReturnsOnCall(0, appEnvelopChan1, errChan)

		discovery.Start(ctx, errChan)

		Eventually(fakeClient.NewAppStreamProviderCallCount).Should(Equal(2))

		Eventually(func() *dto.Metric {
			return test.FindMetric(registry, map[string]string{
				"guid": guid,
			})
		}).ShouldNot(BeNil())
	})

})
