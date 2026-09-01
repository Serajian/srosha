package alert

import "github.com/Serajian/srosha/internal/core/shared"

// message is what an alert looks like when it arrives.
//
// The subject is the title because that is all a lock screen shows, so it has
// to say which thing happened on its own -- "source.create", not "srosha".
func message(address string, it item) shared.Message {
	return shared.Message{
		Recipient: shared.Recipient{Channel: shared.ChannelGotify, Address: address},
		Title:     it.subject,
		Body:      it.detail,
	}
}
