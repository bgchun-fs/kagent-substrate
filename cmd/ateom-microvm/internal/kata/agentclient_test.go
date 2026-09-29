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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

func TestGuestStateRPCs(t *testing.T) {
	for _, tc := range []struct {
		method string
		want   proto.Message
		call   func(context.Context, *AgentClient) error
	}{
		{
			method: "ReseedRandomDev",
			want:   &agentpb.ReseedRandomDevRequest{Data: []byte{1, 2, 3, 4}},
			call: func(ctx context.Context, client *AgentClient) error {
				return client.ReseedRandomDev(ctx, []byte{1, 2, 3, 4})
			},
		},
		{
			method: "SetGuestDateTime",
			want:   &agentpb.SetGuestDateTimeRequest{Sec: 1790693138, Usec: 280976},
			call: func(ctx context.Context, client *AgentClient) error {
				return client.SetGuestDateTime(ctx, time.Unix(1790693138, 280976543))
			},
		},
	} {
		t.Run(tc.method, func(t *testing.T) {
			for _, result := range []struct {
				name string
				err  error
			}{
				{"success", nil},
				{"failure", status.Error(codes.Internal, "guest rejected request")},
			} {
				t.Run(result.name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					requests := make(chan proto.Message, 1)
					client := testAgentClient(t, tc.method, func(_ context.Context, unmarshal func(any) error) (any, error) {
						req := tc.want.ProtoReflect().New().Interface()
						if err := unmarshal(req); err != nil {
							return nil, err
						}
						requests <- req
						return &emptypb.Empty{}, result.err
					})
					err := tc.call(ctx, client)
					if result.err == nil {
						if err != nil {
							t.Fatal(err)
						}
					} else if err == nil || !strings.Contains(err.Error(), tc.method) || status.Code(err) != codes.Internal {
						t.Fatalf("error = %v, want wrapped %s failure", err, tc.method)
					}
					select {
					case got := <-requests:
						if !proto.Equal(got, tc.want) {
							t.Errorf("request = %v, want %v", got, tc.want)
						}
					default:
						t.Fatal("guest did not receive request")
					}
				})
			}
		})
	}
}

func testAgentClient(t *testing.T, method string, handler ttrpc.Method) *AgentClient {
	t.Helper()
	server, err := ttrpc.NewServer()
	if err != nil {
		t.Fatal(err)
	}
	server.RegisterService("grpc.AgentService", &ttrpc.ServiceDesc{Methods: map[string]ttrpc.Method{method: handler}})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- server.Serve(t.Context(), listener) }()
	t.Cleanup(func() {
		if err := server.Close(); err != nil {
			t.Error(err)
		}
		if err := <-done; !errors.Is(err, ttrpc.ErrServerClosed) {
			t.Errorf("server shutdown: %v", err)
		}
	})
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client := &AgentClient{conn: conn, client: ttrpc.NewClient(conn)}
	t.Cleanup(func() { _ = client.Close() })
	return client
}
