package utils

import (
	"io"

	"gopkg.in/gomail.v2"
)

func sendQRCodeViaEmail(qrBuffer []byte, fromEmail string, toEmail string, password string) error {
	m := gomail.NewMessage()
	m.SetHeader("From", fromEmail)
	m.SetHeader("To", toEmail)
	m.SetHeader("Subject", "WhatsApp Pairing Code")
	m.SetBody("text/plain", "Scan the attached QR code to pair the WhatsApp client")

	m.Attach("whatsapp_qr.png", gomail.SetCopyFunc(func(w io.Writer) error {
		_, err := w.Write(qrBuffer)
		return err
	}))

	d := gomail.NewDialer("smtp.gmail.com", 587, fromEmail, password)
	return d.DialAndSend(m)
}
