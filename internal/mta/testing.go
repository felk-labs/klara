//go:build test

package mta

import (
	"context"

	"google.golang.org/grpc"

	"go.klara.mx/internal/pb"
)

type mockMtaTransferCb func(ctx context.Context, req *pb.MtaTransferRequest) (*pb.MtaTransferResponse, error)

type mockMta struct {
	transferCb mockMtaTransferCb

	pb.UnimplementedMtaServer
}

func (m *mockMta) Transfer(ctx context.Context, req *pb.MtaTransferRequest) (*pb.MtaTransferResponse, error) {
	return m.transferCb(ctx, req)
}

func NewMockMtaServer(transferCb mockMtaTransferCb) *grpc.Server {
	srv := grpc.NewServer()
	pb.RegisterMtaServer(srv, &mockMta{transferCb: transferCb})

	return srv
}
