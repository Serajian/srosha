package web

// surface names this one's own directories under public/ -- its templates and
// its assets. The admin surface has its own and cannot reach these.
const surface = "portal"

// Where things live. One list, so a route is never spelled two ways.
const (
	pathHome    = "/"
	pathSignIn  = "/signin"
	pathCode    = "/signin/code"
	pathSignOut = "/signout"
	pathStatic  = "/static"
)

// The pages, by the name a handler asks for them by.
const (
	pageSignIn  = "signin"
	pageCode    = "code"
	pageAccount = "account"
)

// Form fields, spelled once so the template and the handler cannot drift.
const (
	fieldEmail = "email"
	fieldCode  = "code"
)

// The source pages. ":id" is gin's parameter syntax, read with c.Param("id").
const (
	pathSources    = "/sources"
	pathSourceNew  = "/sources/new"
	pathSource     = "/sources/:id"
	pathSourceEdit = "/sources/:id/edit"
	pathSenderOff  = "/sources/:id/senders/:senderID/off"
	pathSenderOn   = "/sources/:id/senders/:senderID/on"
	pathSourceKeys = "/sources/:id/keys"
	pathKeyRevoke  = "/sources/:id/keys/:keyID/revoke"
)

const (
	pageSources    = "sources"
	pageSourceNew  = "source_new"
	pageSource     = "source"
	pageSourceEdit = "source_edit"
	pageKeys       = "keys"
	pageKeyIssued  = "key_issued"
)

const (
	fieldName        = "name"
	fieldDescription = "description"
	fieldChannel     = "channel"
	fieldAddress     = "address"
	fieldLabel       = "label"
)

// Senders and the callback.
const (
	pathSourceSenders  = "/sources/:id/senders"
	pathSourceCallback = "/sources/:id/callback"
)

const (
	pageSenders        = "senders"
	pageCallback       = "callback"
	pageCallbackSecret = "callback_secret"
)

const (
	fieldSecret = "secret"
	fieldConfig = "config"
	fieldURL    = "url"
)
