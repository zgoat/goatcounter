// Copyright © 2019 Martin Tournoij – This file is part of GoatCounter and
// published under the terms of a slightly modified EUPL v1.2 license, which can
// be found in the LICENSE file or at https://license.goatcounter.com

//go:generate go run gen.go

package goatcounter

import (
	"context"
	"embed"
	"fmt"
	"time"

	"zgo.at/zdb"
	"zgo.at/zstd/zcrypto"
)

// DB contains all files in db/*
//
//go:embed db/schema.gotxt
//go:embed db/migrate/*.sql
//go:embed db/query/*.sql
var DB embed.FS

// Static contains all the static files to serve.
//go:embed public/*
var Static embed.FS

// Templates contains all templates.
//go:embed tpl/*
var Templates embed.FS

// GeoDB contains the GeoIP countries database.
//
//go:embed pack/GeoLite2-Country.mmdb.gz
var GeoDB []byte

// State column values.
const (
	StateActive  = "a"
	StateRequest = "r"
	StateDeleted = "d"
)

var States = []string{StateActive, StateRequest, StateDeleted}

// Now gets the current time in UTC; can be overwritten in tests.
var Now = func() time.Time { return time.Now().UTC() }

// TODO: Move to zdb
func interval(ctx context.Context, days int) string {
	if zdb.Driver(ctx) == zdb.DriverPostgreSQL {
		return fmt.Sprintf(" now() - interval '%d days' ", days)
	}
	return fmt.Sprintf(" datetime(datetime(), '-%d days') ", days)
}

const numChars = 12

// Compress all the data in to 12 chunks.
func ChunkStat(stats []HitListStat) (int, []int) {
	var (
		chunked   = make([]int, numChars)
		chunkSize = len(stats) * 24 / numChars
		max       = 0
		chunk     = 0
		i         = 0
		n         = 0
	)
	for _, stat := range stats {
		for _, h := range stat.HourlyUnique {
			i++
			chunk += h
			if i == chunkSize {
				chunked[n] = chunk
				if chunk > max {
					max = chunk
				}
				n++
				chunk, i = 0, 0
			}
		}
	}

	return max, chunked
}

func NewBufferKey(ctx context.Context) (string, error) {
	secret := zcrypto.Secret256()
	err := zdb.TX(ctx, func(ctx context.Context) error {
		err := zdb.Exec(ctx, `delete from store where key='buffer-secret'`, nil)
		if err != nil {
			return err
		}

		err = zdb.Exec(ctx, `insert into store (key, value) values ('buffer-secret', :s)`, zdb.P{"s": secret})
		return err
	})
	if err != nil {
		return "", fmt.Errorf("NewBufferKey: %w", err)
	}
	return secret, nil
}

func LoadBufferKey(ctx context.Context) ([]byte, error) {
	var key []byte
	err := zdb.Get(ctx, &key, `select value from store where key='buffer-secret'`)
	if err != nil {
		return nil, fmt.Errorf("LoadBufferKey: %w", err)
	}
	return key, nil
}
