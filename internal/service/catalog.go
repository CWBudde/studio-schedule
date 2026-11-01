package service

import (
	"context"
	"fmt"
	"slices"

	"github.com/cwbudde/studio-schedule/internal/domain"
	"github.com/cwbudde/studio-schedule/internal/ids"
	"github.com/cwbudde/studio-schedule/internal/store"
)

// CatalogService provides the catalog: locations, rooms, staff, course
// types and levels. Everyone reads it; the office writes it (catalog_admin.go).
type CatalogService struct {
	store store.Store
	ids   ids.Source
}

// NewCatalogService returns a CatalogService backed by st. New records get
// their IDs from src.
func NewCatalogService(st store.Store, src ids.Source) *CatalogService {
	return &CatalogService{store: st, ids: src}
}

// Locations returns all locations ordered by name.
func (s *CatalogService) Locations(ctx context.Context) ([]domain.Location, error) {
	var locations []domain.Location

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		locations, err = tx.Locations().List(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list locations: %w", err)
	}

	return locations, nil
}

// Rooms returns the rooms of a location, or all rooms if location is zero,
// ordered by location name, then by position and name. An unknown location
// is domain.ErrNotFound.
func (s *CatalogService) Rooms(ctx context.Context, location domain.LocationID) ([]domain.Room, error) {
	var rooms []domain.Room

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		// The room list cannot tell an unknown location from one without
		// rooms.
		if !location.IsZero() {
			if _, err := tx.Locations().Get(ctx, location); err != nil {
				return err
			}
		}

		var err error
		rooms, err = tx.Rooms().List(ctx, location)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list rooms: %w", err)
	}

	return rooms, nil
}

// Staff returns the active staff ordered by display name. Former staff
// stay in the store for past lessons but are not listed.
func (s *CatalogService) Staff(ctx context.Context) ([]domain.Staff, error) {
	var staff []domain.Staff

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		staff, err = tx.Staff().List(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list staff: %w", err)
	}

	return slices.DeleteFunc(staff, func(m domain.Staff) bool { return !m.Active }), nil
}

// CourseTypes returns all course types ordered by name.
func (s *CatalogService) CourseTypes(ctx context.Context) ([]domain.CourseType, error) {
	var courseTypes []domain.CourseType

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		courseTypes, err = tx.CourseTypes().List(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list course types: %w", err)
	}

	return courseTypes, nil
}

// Levels returns the school's level scale ordered by position, from least
// to most advanced (ADR 0021).
func (s *CatalogService) Levels(ctx context.Context) ([]domain.Level, error) {
	var levels []domain.Level

	err := s.store.InTx(ctx, store.ReadOnly, func(ctx context.Context, tx store.Tx) error {
		var err error
		levels, err = tx.Levels().List(ctx)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("list levels: %w", err)
	}

	return levels, nil
}
