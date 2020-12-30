class SinglePartInterceptor
  def self.delivering_email(message)
    message.content_transfer_encoding = Mail::Encodings::Identity
    message.parts.delete_if { |part| part.filename.blank? }
  end
end

ActionMailer::Base.register_interceptor(SinglePartInterceptor)
