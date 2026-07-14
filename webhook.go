package golagram

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// maxWebhookBodyBytes caps the size of an incoming webhook request body.
// Real Telegram updates are tiny (well under 1 MB even with maximal
// payloads); this exists to bound memory use against arbitrary POSTs to a
// discovered webhook URL, not to accommodate legitimate traffic near the
// limit.
const maxWebhookBodyBytes = 1 << 20

// WebhookConfig configures [TelegramBot.RunWebhook].
type WebhookConfig struct {
	// Addr is the local address to listen on, e.g. ":8443".
	Addr string
	// Path is the URL path Telegram will POST updates to, e.g.
	// "/telegram/webhook". Must match the path component of PublicURL.
	Path string
	// PublicURL is the full HTTPS URL passed to setWebhook — the address
	// Telegram itself will POST updates to. Required; must be reachable
	// from Telegram's servers (a public HTTPS endpoint, or a tunnel to one
	// in development).
	PublicURL string
	// SecretToken is verified against the X-Telegram-Bot-Api-Secret-Token
	// header Telegram sends on every webhook request — this check is
	// webhook mode's entire security model. Highly recommended: without
	// it, anyone who finds PublicURL can feed the bot fake updates.
	// 1-256 chars, A-Z a-z 0-9 _ - only (Telegram's constraint).
	SecretToken string
	// MaxConnections caps simultaneous HTTPS connections Telegram opens to
	// deliver updates (1-100, Telegram default 40). Zero means "use
	// Telegram's default".
	MaxConnections int64
	// DropPendingUpdates discards any updates queued before the webhook
	// was (re-)set.
	DropPendingUpdates bool
	// DeleteOnStop calls deleteWebhook when RunWebhook's context is
	// canceled. Off by default: most deployments (rolling restarts, brief
	// redeploys) want the webhook to stay registered across a restart, not
	// be torn down and lose updates in the gap.
	DeleteOnStop bool
	// Server, if non-nil, is used as the base *http.Server (for timeouts,
	// custom TLSConfig, etc.) instead of a zero-value one. RunWebhook still
	// sets Addr and Handler on it. Set CertFile/KeyFile (or a TLSConfig with
	// Certificates/GetCertificate already populated) to actually serve TLS
	// — see those fields' docs.
	Server *http.Server
	// CertFile and KeyFile, if both set, make RunWebhook listen with
	// ListenAndServeTLS instead of plain ListenAndServe. Telegram requires
	// HTTPS webhooks: without this (or a TLS-terminating reverse proxy in
	// front of Addr), Telegram silently never delivers a single update —
	// there's no error, the bot just looks idle. Leave both empty when a
	// proxy already terminates TLS in front of this server. Alternatively,
	// set Server.TLSConfig with Certificates or GetCertificate populated
	// and leave these empty; RunWebhook detects that too.
	CertFile string
	KeyFile  string
	// UploadCertificate, if set, is the path to a certificate file read and
	// sent as [SetWebhookRequest.Certificate] when registering the webhook
	// — required so Telegram can verify a self-signed certificate's chain;
	// leave empty for a CA-signed certificate (including one obtained via
	// a reverse proxy).
	UploadCertificate string
}

const secretTokenHeader = "X-Telegram-Bot-Api-Secret-Token"

// secretTokenMatches reports whether got is want, compared in constant time
// so a timing side-channel across many webhook requests can't be used to
// recover the real secret one byte at a time — subtle.ConstantTimeCompare
// requires equal-length slices to report equal, which true byte-for-byte
// equality always satisfies, so this is exactly as correct as a plain ==
// comparison while leaking no timing signal.
func secretTokenMatches(got, want string) bool {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// Handler returns an http.Handler that verifies cfg.SecretToken and feeds
// incoming updates into the same dispatch() (and the same worker pool,
// started by [TelegramBot.RunWebhook]) that polling uses — for embedding
// into an existing server/mux (echo/chi/stdlib) instead of letting
// RunWebhook own the listener. If you use this directly instead of
// RunWebhook, you're responsible for calling [TelegramBot.SetWebhook]
// yourself and for the worker pool: [TelegramBot.StartWorkers] before
// serving, [TelegramBot.StopWorkers] on shutdown.
func (b *TelegramBot) Handler(cfg WebhookConfig) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if cfg.SecretToken != "" && !secretTokenMatches(r.Header.Get(secretTokenHeader), cfg.SecretToken) {
			w.WriteHeader(http.StatusForbidden)
			return
		}

		// Real Telegram updates are tiny; cap the body so a POST to a
		// discovered webhook URL (SecretToken is optional) can't force the
		// process to buffer an arbitrarily large request in memory.
		r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBodyBytes)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				w.WriteHeader(http.StatusRequestEntityTooLarge)
				return
			}
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		var update Update
		if err := json.Unmarshal(raw, &update); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		b.hydrate(&update)

		if b.router != nil {
			c := newCtx(r.Context(), &update, b, b.api, b.fsmStorage, b.botUsername())
			if b.router.matchingAllowsWebhookReply(c) {
				// The matched registration opted into AllowWebhookReply
				// (see its doc): dispatch right here instead of handing off
				// to the worker pool, holding the response open for as long
				// as the handler takes, so a Reply(...) return can be
				// embedded in the response body below instead of costing a
				// follow-up HTTPS call.
				c.attachWebhookReplySink()
				wr := b.dispatchSync(r.Context(), c)
				if wr != nil {
					w.Header().Set("Content-Type", "application/json")
					if err := json.NewEncoder(w).Encode(wr); err != nil {
						b.logErrorf("failed to encode webhook reply: %v", err)
					}
					return
				}
				w.WriteHeader(http.StatusOK)
				return
			}
		}

		// Hand off to the worker pool rather than dispatching inline, so
		// webhook and polling share identical concurrency behavior and the
		// HTTP response isn't held open for however long the handler takes.
		select {
		case b.updateChan <- &update:
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
			// Client gave up (or server is shutting down) before the
			// update could be queued; nothing to acknowledge.
		}
	})
}

// RunWebhook runs the bot in webhook mode: calls [TelegramBot.SetWebhook],
// starts the same worker pool [TelegramBot.Run] (polling) uses, and serves
// cfg.Path until ctx is canceled, then drains in-flight handlers before
// returning — the same shutdown shape as Run. Handlers registered via
// [TelegramBot.Dispatch] are unaffected by which runtime mode delivers
// their updates. One-shot, like Run and StartWorkers (see [TelegramBot]) —
// calling RunWebhook again after a previous Run/RunWebhook/StartWorkers has
// returned returns errAlreadyRan instead of serving.
func (b *TelegramBot) RunWebhook(ctx context.Context, cfg WebhookConfig) error {
	if cfg.PublicURL == "" {
		return fmt.Errorf("golagram: RunWebhook requires a non-empty WebhookConfig.PublicURL")
	}
	if !b.markRan() {
		return errAlreadyRan
	}

	if err := b.runStartupHooks(ctx); err != nil {
		return fmt.Errorf("startup hook: %w", err)
	}

	setReq := &SetWebhookRequest{
		URL:                cfg.PublicURL,
		SecretToken:        cfg.SecretToken,
		MaxConnections:     cfg.MaxConnections,
		DropPendingUpdates: cfg.DropPendingUpdates,
		AllowedUpdates:     b.resolveAllowedUpdates(),
	}
	if cfg.UploadCertificate != "" {
		certFile, err := os.Open(cfg.UploadCertificate)
		if err != nil {
			return fmt.Errorf("open webhook certificate: %w", err)
		}
		setReq.Certificate = InputFileUpload(filepath.Base(cfg.UploadCertificate), certFile)
		_, err = b.SetWebhook(ctx, setReq)
		if closeErr := certFile.Close(); closeErr != nil {
			b.logErrorf("failed to close webhook certificate file: %v", closeErr)
		}
		if err != nil {
			return fmt.Errorf("setWebhook: %w", err)
		}
	} else if _, err := b.SetWebhook(ctx, setReq); err != nil {
		return fmt.Errorf("setWebhook: %w", err)
	}

	b.startWorkers(ctx)
	defer func() {
		b.StopWorkers()
		b.runShutdownHooks(context.Background())
	}()

	mux := http.NewServeMux()
	mux.Handle(cfg.Path, b.Handler(cfg))

	server := cfg.Server
	if server == nil {
		server = &http.Server{}
	}
	server.Addr = cfg.Addr
	server.Handler = mux

	// Telegram requires HTTPS: serve TLS directly when a cert/key (or a
	// pre-populated Server.TLSConfig) was configured, otherwise plain HTTP
	// for deployments that terminate TLS in a reverse proxy in front of Addr.
	tlsConfigured := server.TLSConfig != nil && (len(server.TLSConfig.Certificates) > 0 || server.TLSConfig.GetCertificate != nil)
	errCh := make(chan error, 1)
	go func() {
		if cfg.CertFile != "" && cfg.KeyFile != "" {
			errCh <- server.ListenAndServeTLS(cfg.CertFile, cfg.KeyFile)
		} else if tlsConfigured {
			errCh <- server.ListenAndServeTLS("", "")
		} else {
			errCh <- server.ListenAndServe()
		}
	}()

	select {
	case <-ctx.Done():
		b.logInfo("Context canceled — stopping webhook server gracefully...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("webhook server shutdown: %w", err)
		}
		if cfg.DeleteOnStop {
			if _, err := b.DeleteWebhook(context.Background(), &DeleteWebhookRequest{}); err != nil {
				b.logErrorf("failed to delete webhook on shutdown: %v", err)
			}
		}
		return nil
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	}
}
