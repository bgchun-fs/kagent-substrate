//go:build linux

// Copyright 2026 Google LLC
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package kata

import (
	"bytes"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/cmd/ateom-microvm/internal/third_party/kata/agentpb"
	"github.com/containerd/ttrpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestSynchronizeAfterRestore(t *testing.T) {
	for _, failMethod := range []string{"", "ReseedRandomDev", "SetGuestDateTime"} {
		name := failMethod
		if name == "" {
			name = "fresh entropy and host time on every restore"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			seeds := make(chan []byte, 2)
			clocks := make(chan time.Time, 2)
			server, err := ttrpc.NewServer()
			if err != nil {
				t.Fatal(err)
			}
			server.RegisterService("grpc.AgentService", &ttrpc.ServiceDesc{Methods: map[string]ttrpc.Method{
				"ReseedRandomDev": func(_ context.Context, unmarshal func(any) error) (any, error) {
					var req agentpb.ReseedRandomDevRequest
					if err := unmarshal(&req); err != nil {
						return nil, err
					}
					seeds <- req.Data
					if failMethod == "ReseedRandomDev" {
						return nil, status.Error(codes.Internal, "reseed failed")
					}
					return &emptypb.Empty{}, nil
				},
				"SetGuestDateTime": func(_ context.Context, unmarshal func(any) error) (any, error) {
					var req agentpb.SetGuestDateTimeRequest
					if err := unmarshal(&req); err != nil {
						return nil, err
					}
					if len(seeds) == 0 {
						return nil, status.Error(codes.FailedPrecondition, "clock set before reseed")
					}
					if req.Usec < 0 || req.Usec >= 1_000_000 {
						return nil, status.Error(codes.InvalidArgument, "invalid microseconds")
					}
					clocks <- time.Unix(req.Sec, req.Usec*1000)
					if failMethod == "SetGuestDateTime" {
						return nil, status.Error(codes.Internal, "clock sync failed")
					}
					return &emptypb.Empty{}, nil
				},
			}})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			done := make(chan error, 1)
			go func() { done <- server.Serve(ctx, listener) }()
			t.Cleanup(func() {
				if err := server.Close(); err != nil {
					t.Error(err)
				}
				if err := <-done; !errors.Is(err, ttrpc.ErrServerClosed) {
					t.Errorf("server shutdown: %v", err)
				}
			})
			conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			client := &AgentClient{conn: conn, client: ttrpc.NewClient(conn)}
			t.Cleanup(func() { _ = client.Close() })
			before := time.Now().Truncate(time.Microsecond)
			err = client.SynchronizeAfterRestore(ctx)
			if failMethod != "" {
				if err == nil || !strings.Contains(err.Error(), failMethod) || status.Code(err) != codes.Internal {
					t.Fatalf("error = %v, want wrapped %s failure", err, failMethod)
				}
				if failMethod == "ReseedRandomDev" && len(clocks) != 0 {
					t.Fatal("continued after failed reseed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := client.SynchronizeAfterRestore(ctx); err != nil {
				t.Fatal(err)
			}
			after := time.Now()
			first, second := <-seeds, <-seeds
			if len(first) != 256 || len(second) != 256 || bytes.Equal(first, second) || bytes.Equal(first, make([]byte, 256)) {
				t.Fatal("restores did not receive fresh host entropy")
			}
			for range 2 {
				if clock := <-clocks; clock.Before(before) || clock.After(after) {
					t.Errorf("guest clock %s outside host interval [%s, %s]", clock, before, after)
				}
			}
		})
	}
}
