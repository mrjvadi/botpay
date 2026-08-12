// This file defines the NATS contract between source-service (an internal
// MTProto/UserBot automation tool — NOT a customer-facing BotInstance like
// uploader/vpn/archive/member) and botmanager. Only trusted core services
// hold a ServiceKey and may call source.worker.register — this mirrors the
// same trust model as SubjLicenseIssue/SubjPayCredit above: a
// ServiceID/ServiceKey pair authenticates the caller, not a per-message
// secret embedded in every task call (task subjects like worker.<id>.tasks
// are expected to be restricted at the NATS-account/permission level
// instead, since they're called far more often and by fewer distinct
// callers).
//
// Unlike the license.* subjects, this is not anti-clone verification — it's
// how a source-service worker instance, on startup, learns which Telegram
// account (phone/app credentials) and worker ID it should operate as, and
// how it reports results back for botmanager to relay to user-facing bots
// when relevant (rare — most source-service output stays internal).
package protocol

const (
	// SubjSourceWorkerRegister — a source-service instance activates a
	// LicenseKey (which account/config to run as) and learns its worker ID.
	// Request/reply. Responder: botmanager.
	SubjSourceWorkerRegister = "source.worker.register"

	// SubjSourceWorkerHeartbeat — periodic liveness ping, worker -> botmanager.
	// Fire-and-forget.
	SubjSourceWorkerHeartbeat = "source.worker.heartbeat"

	// SubjSourceWorkerUpdate — a worker reports a result back, tagged with
	// the correlation ID of whatever instruction produced it (see
	// source-service's internal/task.Envelope.ID). botmanager decides
	// whether/how to relay this to a user-facing bot. Request/reply.
	// Responder: botmanager.
	SubjSourceWorkerUpdate = "source.worker.update"
)

// SourceWorkerRegisterRequest activates a license/config slot for this
// worker instance.
type SourceWorkerRegisterRequest struct {
	ServiceID  string `json:"service_id"`  // "source-service"
	ServiceKey string `json:"service_key"` // HMAC(SERVICE_HMAC_SECRET, service_id) — same convention as pay/license
	LicenseKey string `json:"license_key"` // identifies which configured Telegram account/instance to activate
}

// SourceWorkerTelegramCreds is the Telegram account a worker should operate
// as, and the key it should encrypt its MTProto session with at rest.
type SourceWorkerTelegramCreds struct {
	AppID      int    `json:"app_id"`
	AppHash    string `json:"app_hash"`
	Phone      string `json:"phone"`
	SessionKey string `json:"session_key"` // base64, AES-256
}

// SourceWorkerRegisterResponse is botmanager's reply to SubjSourceWorkerRegister.
type SourceWorkerRegisterResponse struct {
	Success  bool                      `json:"success"`
	WorkerID string                    `json:"worker_id,omitempty"`
	Telegram SourceWorkerTelegramCreds `json:"telegram,omitempty"`
	Error    string                    `json:"error,omitempty"`
}

// SourceWorkerHeartbeat is published (fire-and-forget) by a worker on
// SubjSourceWorkerHeartbeat.
type SourceWorkerHeartbeat struct {
	ServiceID     string `json:"service_id"`
	ServiceKey    string `json:"service_key"`
	WorkerID      string `json:"worker_id"`
	Status        string `json:"status"`
	UptimeSeconds int    `json:"uptime_seconds"`
	Timestamp     int64  `json:"timestamp"`
}

// SourceWorkerUpdateRequest reports a task's real result — possibly
// produced asynchronously, after several steps — back to botmanager.
type SourceWorkerUpdateRequest struct {
	ServiceID  string         `json:"service_id"`
	ServiceKey string         `json:"service_key"`
	ID         string         `json:"id"`   // matches the originating task.Envelope.ID
	Tags       map[string]any `json:"tags"` // e.g. {"action":"bot_file_ready","archive_file_id":"...",...}
}

// SourceWorkerUpdateResponse is botmanager's reply to SubjSourceWorkerUpdate.
type SourceWorkerUpdateResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}
