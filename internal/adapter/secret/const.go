package secret

// aadSeparator joins the three fields that a sealed secret is bound to.
//
// None of them can contain it: two are ULIDs and the third is a channel from a
// closed set. So there is exactly one way to read the joined string, and a
// credential cannot be made to produce the binding of another by choosing a
// clever name -- names are not in it at all, because a name can be changed and
// the binding must not.
const aadSeparator = "|"
