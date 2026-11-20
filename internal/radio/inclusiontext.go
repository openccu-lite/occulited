package radio

import (
	"strconv"
	"strings"
)

// PairingText is a failed pairing's sentence in the two languages (D-12). The pairing API answers
// it with every attempt, so that a pairing client says what the system says without a table of its
// own; the Status page's warning hmip-pairing-failed uses the same sentences from the UI's
// catalogue, and a test holds the two to each other.
type PairingText struct {
	EN string `json:"en"`
	DE string `json:"de"`
}

// pairingStatusSuffix is the key-server-unreachable reason's sentence when the key server answered
// with an HTTP status other than 200 (D-106 counts that as not reached: a proxy or a captive
// portal answers so as well).
const pairingStatusSuffix = "-status"

// pairingTexts are the sentences by reason; {sgtin} and {status} are filled in.
var pairingTexts = map[string]PairingText{
	PairingKeyMismatch: {
		EN: "The device {sgtin} answered, but the key given to the system for it (entered for the pairing, or stored on the Keys page) does not match it. Check the key (QR code or KEY on its sticker) and pair it again; if it no longer tries, reset it to its factory settings first.",
		DE: "Das Gerät {sgtin} hat geantwortet, aber der dem System dafür angegebene Schlüssel (beim Anlernen eingegeben oder auf der Seite Schlüssel gespeichert) passt nicht zu ihm. Den Schlüssel prüfen (QR-Code oder KEY auf dem Aufkleber) und das Gerät erneut anlernen; versucht es das nicht mehr, vorher auf Werkseinstellungen zurücksetzen.",
	},
	PairingKeyInvalid: {
		EN: "The key stored for the device {sgtin} is no valid device key (wrong length). Remove it on the Keys page, scan or type the key from the device's sticker again, apply it, and pair the device once more.",
		DE: "Der für das Gerät {sgtin} gespeicherte Schlüssel ist kein gültiger Geräteschlüssel (falsche Länge). Ihn auf der Seite Schlüssel entfernen, den Schlüssel vom Aufkleber des Geräts erneut scannen oder eintippen, übernehmen und das Gerät noch einmal anlernen.",
	},
	PairingOtherDevice: {
		EN: "The device {sgtin} asked to join, but the pairing was open for another SGTIN. Compare the SGTIN entered for the pairing with the device's sticker and start the pairing again.",
		DE: "Das Gerät {sgtin} wollte sich anmelden, aber das Anlernen war für eine andere SGTIN geöffnet. Die beim Anlernen eingegebene SGTIN mit dem Aufkleber des Geräts vergleichen und das Anlernen neu starten.",
	},
	PairingNoInstallMode: {
		EN: "The device {sgtin} asked to join while no pairing was open. Start the pairing first, then put the device into pairing mode.",
		DE: "Das Gerät {sgtin} wollte sich anmelden, während kein Anlernen geöffnet war. Zuerst das Anlernen starten, dann das Gerät in den Anlernmodus bringen.",
	},
	PairingUnsupported: {
		EN: "The device {sgtin} asked to join, but the system's HmIP software does not know its device type yet, so it cannot be paired. A system update with a newer HmIP software brings it.",
		DE: "Das Gerät {sgtin} wollte sich anmelden, aber die HmIP-Software des Systems kennt seinen Gerätetyp noch nicht, daher kann es nicht angelernt werden. Ein System-Update mit einer neueren HmIP-Software bringt ihn mit.",
	},
	PairingNoKey: {
		EN: "The device {sgtin} asked to join, but the system has no key for it, and in local key mode eQ-3's key server is never asked. Pair it with its key (SGTIN and KEY from its sticker), or store the key on the Keys page first.",
		DE: "Das Gerät {sgtin} wollte sich anmelden, aber das System hat keinen Schlüssel dafür, und im lokalen Schlüsselmodus wird der Schlüsselserver von eQ-3 nie gefragt. Das Gerät mit seinem Schlüssel anlernen (SGTIN und KEY vom Aufkleber) oder den Schlüssel vorher auf der Seite Schlüssel speichern.",
	},
	PairingKeyServerRefused: {
		EN: "eQ-3's key server did not accept the device {sgtin}. Usually it is still paired with another central unit (a CCU or the Homematic IP app): remove it there, reset it to its factory settings, and pair it again.",
		DE: "Der Schlüsselserver von eQ-3 hat das Gerät {sgtin} nicht angenommen. Meist ist es noch an einer anderen Zentrale angelernt (einer CCU oder der Homematic IP App): es dort entfernen, auf Werkseinstellungen zurücksetzen und erneut anlernen.",
	},
	PairingKeyServerUnreachable: {
		EN: "The device {sgtin} asked to join, but the system could not reach eQ-3's key server (secgtw.homematic.com, port 8443). Check the internet connection, the DNS settings and the firewall, then pair it again.",
		DE: "Das Gerät {sgtin} wollte sich anmelden, aber das System hat den Schlüsselserver von eQ-3 nicht erreicht (secgtw.homematic.com, Port 8443). Die Internetverbindung, die DNS-Einstellungen und die Firewall prüfen, dann das Gerät erneut anlernen.",
	},
	PairingKeyServerUnreachable + pairingStatusSuffix: {
		EN: "The device {sgtin} asked to join, but eQ-3's key server (secgtw.homematic.com, port 8443) answered with HTTP status {status} instead of the device's key. Check the internet connection, a proxy and the firewall, then pair it again; the key server may also be down for a while.",
		DE: "Das Gerät {sgtin} wollte sich anmelden, aber der Schlüsselserver von eQ-3 (secgtw.homematic.com, Port 8443) hat mit dem HTTP-Status {status} statt mit dem Schlüssel des Geräts geantwortet. Die Internetverbindung, einen Proxy und die Firewall prüfen, dann das Gerät erneut anlernen; der Schlüsselserver kann auch zeitweise gestört sein.",
	},
	PairingIncomplete: {
		EN: "The device {sgtin} asked to join, but the pairing did not complete. Keep it close to the system, put it into pairing mode again and pair it once more; if that fails as well, reset it to its factory settings first.",
		DE: "Das Gerät {sgtin} wollte sich anmelden, aber das Anlernen wurde nicht abgeschlossen. Das Gerät nahe am System in den Anlernmodus bringen und erneut anlernen; schlägt das wieder fehl, vorher auf Werkseinstellungen zurücksetzen.",
	},
}

// PairingMessage is the sentence for a reason, the device and the key server's HTTP status filled
// in; a zero value for a reason without one (a pairing that worked).
func PairingMessage(reason, sgtin string, httpStatus int) PairingText {
	key := reason
	if reason == PairingKeyServerUnreachable && httpStatus != 0 {
		key += pairingStatusSuffix
	}
	t, ok := pairingTexts[key]
	if !ok {
		return PairingText{}
	}
	r := strings.NewReplacer("{sgtin}", sgtin, "{status}", strconv.Itoa(httpStatus))
	return PairingText{EN: r.Replace(t.EN), DE: r.Replace(t.DE)}
}
