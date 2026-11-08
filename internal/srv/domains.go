package srv

import (
	"context"
	"errors"

	"github.com/klara-to/mx/internal/db"
	"github.com/klara-to/mx/pb"
)

type domains struct {
	pb.UnimplementedDomainsServer
}

func (*domains) Create(ctx context.Context, req *pb.DomainsCreateRequest) (*pb.DomainsCreateResponse, error) {
	domain, err := db.NewDomain(ctx, req.GetName(), db.DomainAttrs{})
	if err != nil {
		if errors.Is(err, db.ErrInvalidData) {
			return nil, ErrInvalidData
		}
		if errors.Is(err, db.ErrAlreadyExists) {
			return nil, ErrAlreadyExists
		}

		return nil, err
	}

	return &pb.DomainsCreateResponse{
		Domain: mapDomainToPb(domain),
	}, nil
}

func mapDomainToPb(d *db.Domain) *pb.Domain {
	return &pb.Domain{
		Id:   d.Id(),
		Name: d.Name(),
	}
}
