// Package server assembles the application: the list of features whose
// routes make up the API. main and the tests build the same list, so a
// route that is registered here is automatically covered by the
// cross-household test (crosshousehold_test.go).
package server

import (
	"context"
	"io/fs"
	"net/http"
	"time"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/drafts"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/jobs"
	"github.com/kaanemec/things-items-inventory/internal/photos"
	"github.com/kaanemec/things-items-inventory/internal/quickadd"
	"github.com/kaanemec/things-items-inventory/internal/search"
	"github.com/kaanemec/things-items-inventory/internal/stream"
)

// SeedResult is what a feature's Seed reports about the rows it created.
type SeedResult struct {
	// PathValues are IDs of the rows just created (item, place, photo,
	// ...). The cross-household test substitutes each of them, one at a
	// time, into every {param} of every route, as a member of another
	// household, and expects not-found.
	PathValues []string
	// Markers are strings that appear in the new rows' JSON (names, notes).
	// They must never show up in any response to the other household.
	Markers []string
}

// Feature is one package's contribution to the API.
type Feature struct {
	Name   string
	Routes httpapi.Routes
	// Seed creates a few rows owned by householdID, for the cross-household
	// test. Optional, but a feature with household-owned rows should
	// provide it, otherwise the test can only prove that unknown IDs give
	// 404, not that real rows of another household do. Use unique marker
	// text for names.
	Seed func(ctx context.Context, d *db.DB, householdID string) (SeedResult, error)
}

// Features lists every feature, auth first. To add a feature package, append
// a Feature here: its routes then appear in the app and in the
// cross-household test, with no change to the test. The job queue it creates
// is not started; New returns one that is.
func Features(d *db.DB, cfg config.Config) []Feature {
	hub := stream.New()
	return features(d, cfg, jobs.New(d, jobs.Options{OnChange: hub.OnJob}), hub)
}

func features(d *db.DB, cfg config.Config, q *jobs.Queue, hub *stream.Hub) []Feature {
	hub.AttachInventory()
	authSvc := auth.New(d, cfg)
	authSvc.AddSetupHook(inventory.SeedStarter)
	inventorySvc := inventory.New(d)
	aiSvc := ai.NewService(d, cfg)
	quickaddSvc := quickadd.New(d, aiSvc)
	photoSvc := photos.New(d, cfg)
	searchSvc := search.New(d) // subscribes the index to inventory.OnChange
	photoSvc.UseQueue(q)
	draftsSvc := drafts.New(d, aiSvc, photoSvc, q)
	linkPhotos(d, photoSvc, draftsSvc)
	return []Feature{
		{Name: "auth", Routes: authSvc.Routes},
		{Name: "ai", Routes: aiSvc.Routes, Seed: func(ctx context.Context, d *db.DB, hid string) (SeedResult, error) {
			m, err := aiSvc.Seed(ctx, d, hid)
			return SeedResult{Markers: m}, err
		}},
		{Name: "quickadd", Routes: quickaddSvc.Routes, Seed: func(ctx context.Context, d *db.DB, hid string) (SeedResult, error) {
			m, err := quickaddSvc.Seed(ctx, d, hid)
			return SeedResult{Markers: m}, err
		}},
		{Name: "inventory", Routes: inventorySvc.Routes, Seed: func(ctx context.Context, d *db.DB, hid string) (SeedResult, error) {
			paths, markers, err := inventory.SeedTestData(ctx, d, hid)
			return SeedResult{PathValues: paths, Markers: markers}, err
		}},
		{Name: "photos", Routes: photoSvc.Routes, Seed: func(ctx context.Context, d *db.DB, hid string) (SeedResult, error) {
			paths, markers, err := photoSvc.Seed(ctx, d, hid)
			return SeedResult{PathValues: paths, Markers: markers}, err
		}},
		{Name: "search", Routes: searchSvc.Routes, Seed: func(ctx context.Context, d *db.DB, hid string) (SeedResult, error) {
			m, err := searchSvc.Seed(ctx, d, hid)
			return SeedResult{Markers: m}, err
		}},
		{Name: "drafts", Routes: draftsSvc.Routes, Seed: func(ctx context.Context, d *db.DB, hid string) (SeedResult, error) {
			paths, markers, err := draftsSvc.Seed(ctx, d, hid)
			return SeedResult{PathValues: paths, Markers: markers}, err
		}},
		{Name: "stream", Routes: hub.Routes},
		jobsFeature(q),
	}
}

// Routes returns the Routes of every feature, for httpapi.New.
func Routes(features []Feature) []httpapi.Routes {
	out := make([]httpapi.Routes, len(features))
	for i, f := range features {
		out[i] = f.Routes
	}
	return out
}

// Handler builds the complete HTTP handler with a queue that is not started.
func Handler(version, buildTime string, app fs.FS, d *db.DB, cfg config.Config) http.Handler {
	return httpapi.New(version, buildTime, app, Routes(Features(d, cfg))...)
}

// App is the running application: the handler and the background queue.
type App struct {
	Handler http.Handler
	Queue   *jobs.Queue
	Hub     *stream.Hub
}

// New builds the application. Call Start before serving and Stop on shutdown.
func New(version, buildTime string, app fs.FS, d *db.DB, cfg config.Config) *App {
	hub := stream.New()
	q := jobs.New(d, jobs.Options{OnChange: hub.OnJob})
	return &App{Queue: q, Hub: hub, Handler: httpapi.New(version, buildTime, app, Routes(features(d, cfg, q, hub))...)}
}

// Start starts the job workers; they stop when ctx ends or Stop is called.
func (a *App) Start(ctx context.Context) error { return a.Queue.Start(ctx) }

// Stop stops the workers, giving running jobs a few seconds to finish.
func (a *App) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return a.Queue.Stop(ctx)
}
