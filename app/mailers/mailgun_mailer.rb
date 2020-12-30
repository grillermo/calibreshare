class MailgunMailer < ApplicationMailer
  # to_attrributes
  # [:to, "john.doe@example.com", {"first" => "John", "last" => "Doe"}]
  #
  # from_attributes:
  # ["me@example.com", {"first"=>"Ruby", "last" => "SDK"}]
  #
  def mailgun_mail(
    from_attributes:,
    to_attributes:,
    subject:,
      attachment_url:,
    attachment_name:,
    body:
  )
    file = open(attachment_name, 'wb') do |file|
      file << open(attachment_url).read
    end

    mg_client = Mailgun::Client.new
    mb_obj = Mailgun::MessageBuilder.new

    mb_obj.from(*from_attributes)
    mb_obj.add_recipient(*to_attributes)
    mb_obj.subject(subject)
    mb_obj.set_text_body(body)
    mb_obj.add_attachment(file.path)

    response = mg_client.send_message(ENV['DEFAULT_EMAIL'], mb_obj)

    File.delete(file.path)
    response
  end
end
