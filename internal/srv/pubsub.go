package srv

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/rs/zerolog"

	"go.klara.mx/internal/events"
	"go.klara.mx/internal/mda"
	"go.klara.mx/internal/mta"
	pbi "go.klara.mx/internal/pb"
	"go.klara.mx/pb"
)

type pubsubMessage struct {
	Attributes  map[string]string `json:"attributes,omitempty"`
	Data        []byte            `json:"data,omitempty"`
	MessageId   string            `json:"message_id,omitempty"`
	PublishTime time.Time         `json:"publish_time"`

	OrderingKey string `json:"orderingKey,omitempty"`
}

type pubsubRequest struct {
	DeliveryAttempt int           `json:"deliveryAttempt,omitempty"`
	Message         pubsubMessage `json:"message"`
	Subscription    string        `json:"subscription,omitempty"`
}

func pubsubHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		w.Header().Set("Accept-Post", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusUnsupportedMediaType)
		return
	}

	log := zerolog.Ctx(r.Context())

	var req pubsubRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Warn().Err(err).Msg("failed to decode pubsub message")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if req.Message.Data == nil {
		log.Warn().Str("message_id", req.Message.MessageId).Msg("empty pubsub message data")
		w.WriteHeader(http.StatusNoContent)
		return
	}

	ev, err := events.Unmarshal(req.Message.Data)
	if err != nil {
		log.Warn().Err(err).Msg("failed to unmarshal event")
		w.WriteHeader(http.StatusBadRequest)
	}

	pbEv := ev.Unwrap()
	switch pbEv.Data.(type) {
	case *pbi.Event_Mail:
		pbMail := pbEv.GetMail()
		switch pbMail.GetType() {
		case pb.MailType_mail_type_inbound:
			err = mda.Deliver(r.Context(), pbMail.GetId())
		case pb.MailType_mail_type_outbound:
			err = mta.Transfer(r.Context(), pbMail.GetId())
		}

		if err != nil {
			log.Error().Err(err).Msg("failed to process mail event")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

	case *pbi.Event_Msg:
		w.WriteHeader(http.StatusNoContent)
		return

	default:
		name, _ := pbEv.GetAttrs()[events.AttrsEventNameKey]
		log.Warn().
			Str(events.AttrsEventNameKey, name).
			Msg("no processing logic for event")

		w.WriteHeader(http.StatusNotImplemented)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
