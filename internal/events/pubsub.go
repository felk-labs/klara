package events

import (
	"context"

	"cloud.google.com/go/pubsub/v2"
	"github.com/rs/zerolog"
)

type pubsubPublisher struct {
	client    *pubsub.Client
	publisher *pubsub.Publisher
}

func (p *pubsubPublisher) Publish(ctx context.Context, ev *Event) error {
	b, err := ev.Marshal()
	if err != nil {
		return err
	}

	r := p.publisher.Publish(ctx, &pubsub.Message{
		Attributes: ev.attrs(),
		Data:       b,
	})

	// Log any errors
	go func() {
		log := zerolog.Ctx(ctx)
		ctx, cancel := context.WithTimeout(context.Background(), publisherWaitTimeout)
		defer cancel()

		_, err := r.Get(ctx)
		if err != nil {
			log.Warn().Err(err).Msg("failed to publish event")
		}
	}()

	return nil
}

func (p *pubsubPublisher) PublishSync(ctx context.Context, ev *Event) error {
	b, err := ev.Marshal()
	if err != nil {
		return err
	}

	r := p.publisher.Publish(ctx, &pubsub.Message{
		Attributes: ev.attrs(),
		Data:       b,
	})

	_, err = r.Get(ctx)
	if err != nil {
		return err
	}

	return nil
}

func (p *pubsubPublisher) Stop() {
	p.publisher.Stop()
}

func initPubsub(ctx context.Context, projectId string, topic string) error {
	if projectId == "" {
		projectId = pubsub.DetectProjectID
	}

	c, err := pubsub.NewClient(ctx, projectId)
	if err != nil {
		return err
	}

	publisher = &pubsubPublisher{
		client:    c,
		publisher: c.Publisher(topic),
	}

	return nil
}

func stopPubsub() {
	if publisher == nil {
		return
	}

	publisher.Stop()
}
