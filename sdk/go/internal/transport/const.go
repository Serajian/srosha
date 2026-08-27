package transport

// authHeader is how a key is presented, and it is lower case because gRPC
// metadata keys always are. Sending "Authorization" would be read as no key at
// all.
const authHeader = "authorization"

// bearerPrefix matches what the service parses. It reads the scheme without
// regard to case, but there is no reason to send anything but the usual form.
const bearerPrefix = "bearer "
