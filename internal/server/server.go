// Package server assembles the application: the list of features whose
// routes make up the API. main and the tests build the same list, so a
// route that is registered here is automatically covered by the
// cross-household test (crosshousehold_test.go).
package server

import (
	"context"
	"io/fs"
	"net/http"

	"github.com/kaanemec/things-items-inventory/internal/ai"
	"github.com/kaanemec/things-items-inventory/internal/auth"
	"github.com/kaanemec/things-items-inventory/internal/config"
	"github.com/kaanemec/things-items-inventory/internal/db"
	"github.com/kaanemec/things-items-inventory/internal/httpapi"
	"github.com/kaanemec/things-items-inventory/internal/inventory"
	"github.com/kaanemec/things-items-inventory/internal/photos"
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
// cross-household test, with no change to the test.
func Features(d *db.DB, cfg config.Config) []Feature {
	authSvc := auth.New(d, cfg)
	authSvc.AddSetupHook(inventory.SeedStarter)
	inventorySvc := inventory.New(d)
	aiSvc := ai.NewService(d, cfg)
	photoSvc := photos.New(d, cfg)
	return []Feature{
		{Name: "auth", Routes: authSvc.Routes},
		{Name: "ai", Routes: aiSvc.Routes, Seed: func(ctx context.Context, d *db.DB, hid string) (SeedResult, error) {
			m, err := aiSvc.Seed(ctx, d, hid)
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

// Handler builds the complete HTTP handler.
func Handler(version, buildTime string, app fs.FS, d *db.DB, cfg config.Config) http.Handler {
	return httpapi.New(version, buildTime, app, Routes(Features(d, cfg))...)
}
