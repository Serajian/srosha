package settings

// minRetentionMultiple is how much longer a message is kept than a delivery is
// tried for.
//
// Retention deletes by age alone -- no check that the deliveries settled -- and
// that reasoning holds only while the two numbers are far apart: a delivery
// gives up in minutes, so one still pending a month later is a row recovery
// never saw rather than work waiting to happen. Bring them close and the sweep
// starts deleting messages that would still have gone out.
//
// With the defaults the real ratio is over a thousand, so this is never in the
// way. It is here for the day somebody changes one number without the other.
const minRetentionMultiple = 24
