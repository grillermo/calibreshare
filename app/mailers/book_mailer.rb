require 'open-uri'

class BookMailer < MailgunMailer
  default from: ENV['DEFAULT_EMAIL']

  def send_ebook(url, name, email)
    mailgun_mail(
      to_attributes: [:to, email],
      from_attributes: [
        default_params[:from],
        first: 'Grille',
        last: 'envios'
      ],
      attachment_url: url,
      attachment_name: name,
      subject: name,
      body: "Tu libro desde #{ENV['APPLICATION_NAME']}",
    )
  end
end
