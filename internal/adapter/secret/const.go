package secret

// aadSeparator joins the three fields that a sealed secret is bound to.
//
// None of them can contain it: two are ULIDs and the third is a channel from a
// closed set. So there is exactly one way to read the joined string, and a
// credential cannot be made to produce the binding of another by choosing a
// clever name -- names are not in it at all, because a name can be changed and
// the binding must not.
const aadSeparator = "|"

// signingSecretBytes is the entropy behind a callback's signing secret. The
// same 256 bits an api key gets, and for the same reason: it is the only thing
// standing between a receiver and a forged callback.
const signingSecretBytes = 32

// signingSecretPrefix marks one for what it is, so a secret pasted somewhere it
// does not belong is recognizable at a glance.
const signingSecretPrefix = "whsec_"
