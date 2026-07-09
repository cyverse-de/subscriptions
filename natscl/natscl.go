package natscl

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/cyverse-de/go-mod/gotelnats"
	"github.com/cyverse-de/go-mod/logging"
	"github.com/nats-io/nats.go"
	"github.com/sirupsen/logrus"
)

var log = logging.Log.WithFields(logrus.Fields{"package": "natscl"})

type ConnectionSettings struct {
	ClusterURLS   string
	CredsPath     string
	CredsEnabled  bool
	TLSCACertPath string
	TLSCertPath   string
	TLSKeyPath    string
	TLSEnabled    bool
	MaxReconnects int
	ReconnectWait int
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		log.Panicf("unable to check for the existence of %s", path)
	}
	return true
}

func (s *ConnectionSettings) toConnectOptions() []nats.Option {
	options := make([]nats.Option, 0)

	// Add the credentials if a path is specified and it exists.
	if s.CredsEnabled && s.CredsPath != "" && fileExists(s.CredsPath) {
		options = append(options, nats.UserCredentials(s.CredsPath))
	}

	// Add the TLS settings if we're supposed to.
	if s.TLSEnabled {
		options = append(options, nats.RootCAs(s.TLSCACertPath))
		options = append(options, nats.ClientCert(s.TLSCertPath, s.TLSKeyPath))
	}

	// Add the rest of the options.
	options = append(options, nats.RetryOnFailedConnect(true))
	options = append(options, nats.MaxReconnects(s.MaxReconnects))
	options = append(options, nats.ReconnectWait(time.Duration(s.ReconnectWait)*time.Second))

	// A handler funciton to log error messages when the NATS connection is dropped.
	options = append(options, nats.DisconnectErrHandler(
		func(nc *nats.Conn, err error) {
			if err != nil {
				log.Errorf("disconnected from nats: %s", err.Error())
			}
		},
	))

	// A handler function to log an informational message when the NATS connection is restored.
	options = append(options, nats.ReconnectHandler(
		func(nc *nats.Conn) {
			log.Infof("reconnected to %s", nc.ConnectedUrl())
		},
	))

	// A handler function to log an informational message when the NATS connection is closed.
	options = append(options, nats.ClosedHandler(
		func(nc *nats.Conn) {
			log.Errorf("connection closed: %s", nc.LastError().Error())
		},
	))

	return options
}

//nolint:staticcheck
func NewConnection(settings *ConnectionSettings) (*nats.EncodedConn, error) {
	log := log.WithFields(logrus.Fields{"context": "new nats conn"})

	log.Infof("establishing the NATS connection: %s", settings.ClusterURLS)

	nc, err := nats.Connect(settings.ClusterURLS, settings.toConnectOptions()...)
	if err != nil {
		return nil, err
	}

	encConn, err := nats.NewEncodedConn(nc, nats.JSON_ENCODER)
	if err != nil {
		return nil, err
	}

	return encConn, nil
}

//nolint:staticcheck
type Client struct {
	conn          *nats.EncodedConn
	subscriptions []*nats.Subscription
	queueSuffix   string
}

//nolint:staticcheck
func NewClient(conn *nats.EncodedConn, queueSuffix string) *Client {
	return &Client{
		conn:          conn,
		queueSuffix:   queueSuffix,
		subscriptions: make([]*nats.Subscription, 0),
	}
}

func (c *Client) queueName(base string) string {
	return strings.Join([]string{base, c.queueSuffix}, ".")
}

//nolint:staticcheck
func (c *Client) Subscribe(subject string, handler nats.Handler) error {
	queue := c.queueName(subject)

	s, err := c.conn.QueueSubscribe(subject, queue, handler)
	if err != nil {
		return err
	}

	c.subscriptions = append(c.subscriptions, s)

	log.Infof("added handler for subject %s on queue %s", subject, queue)

	return nil
}

func (c *Client) Respond(ctx context.Context, replySubject string, response gotelnats.DEResponse) error {
	return gotelnats.PublishResponse(ctx, c.conn, replySubject, response)
}
