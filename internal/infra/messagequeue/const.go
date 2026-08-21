package messagequeue

// reconnectForever is what nats reads any negative MaxReconnects as. Naming it
// keeps the meaning of -1 out of a comment and in the code.
const reconnectForever = -1
