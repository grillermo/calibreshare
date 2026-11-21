package main

import (
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"time"
)

// Mailer sends books through Mailgun's HTTP API. The attachment is streamed
// from the library host straight into the Mailgun request, so a book is never
// held in memory or written to disk.
type Mailer struct {
	APIBase string // https://api.mailgun.net/v3
	APIKey  string
	Domain  string
	From    string // "Grille envios <books@example.com>"
	AppName string
	Client  *http.Client
}

func (m *Mailer) SendBook(fileURL, fileName, to string) error {
	src, err := m.Client.Get(fileURL)
	if err != nil {
		return fmt.Errorf("downloading %s: %w", fileURL, err)
	}
	defer src.Body.Close()
	if src.StatusCode != http.StatusOK {
		return fmt.Errorf("downloading %s: %s", fileURL, src.Status)
	}

	pr, pw := io.Pipe()
	form := multipart.NewWriter(pw)
	go func() {
		err := func() error {
			fields := [][2]string{
				{"from", m.From},
				{"to", to},
				{"subject", fileName},
				{"text", "Tu libro desde " + m.AppName},
			}
			for _, f := range fields {
				if err := form.WriteField(f[0], f[1]); err != nil {
					return err
				}
			}
			part, err := form.CreateFormFile("attachment", fileName)
			if err != nil {
				return err
			}
			if _, err := io.Copy(part, src.Body); err != nil {
				return err
			}
			return form.Close()
		}()
		pw.CloseWithError(err)
	}()

	req, err := http.NewRequest(http.MethodPost, m.APIBase+"/"+m.Domain+"/messages", pr)
	if err != nil {
		pr.CloseWithError(err)
		return err
	}
	req.SetBasicAuth("api", m.APIKey)
	req.Header.Set("Content-Type", form.FormDataContentType())

	resp, err := m.Client.Do(req)
	if err != nil {
		return fmt.Errorf("mailgun: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("mailgun: %s: %s", resp.Status, body)
	}
	return nil
}

// SendBookAsync sends in the background, like Rails' deliver_later.
func (m *Mailer) SendBookAsync(fileURL, fileName, to string) {
	go func() {
		start := time.Now()
		if err := m.SendBook(fileURL, fileName, to); err != nil {
			log.Printf("send %q to %s failed: %v", fileName, to, err)
			return
		}
		log.Printf("sent %q to %s in %s", fileName, to, time.Since(start).Round(time.Millisecond))
	}()
}
