/*
Copyright 2026 Tony Owens.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"testing"

	opendepotv1alpha1 "github.com/tonedefdev/opendepot/api/v1alpha1"
)

func Test_anyVersionUnsynced(t *testing.T) {
	tests := []struct {
		name string
		vs   []opendepotv1alpha1.Version
		want bool
	}{
		{
			name: "empty slice returns false",
			vs:   []opendepotv1alpha1.Version{},
			want: false,
		},
		{
			name: "all versions synced",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
			},
			want: false,
		},
		{
			name: "one version has synced false",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
				{Status: opendepotv1alpha1.VersionStatus{Synced: false, SyncStatus: "Not synced"}},
			},
			want: true,
		},
		{
			name: "syncStatus contains failed",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "sync failed: storage unavailable"}},
			},
			want: true,
		},
		{
			name: "syncStatus contains error",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "error uploading artifact"}},
			},
			want: true,
		},
		{
			name: "syncStatus failed case-insensitive",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Failed: network timeout"}},
			},
			want: true,
		},
		{
			name: "syncStatus error case-insensitive",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Error: 403 Forbidden"}},
			},
			want: true,
		},
		{
			name: "synced true but error in syncStatus triggers warning",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "error: checksum mismatch"}},
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
			},
			want: true,
		},
		{
			name: "mixed: first synced, second not",
			vs: []opendepotv1alpha1.Version{
				{Status: opendepotv1alpha1.VersionStatus{Synced: true, SyncStatus: "Synced"}},
				{Status: opendepotv1alpha1.VersionStatus{Synced: false, SyncStatus: "failed: upload error"}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := anyVersionUnsynced(tt.vs)
			if got != tt.want {
				t.Errorf("anyVersionUnsynced() = %v, want %v", got, tt.want)
			}
		})
	}
}
