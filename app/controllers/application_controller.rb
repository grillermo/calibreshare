class ApplicationController < ActionController::Base
  helper_method :email_registered?, :registered_email

  def email_registered?
    registered_email.present?
  end

  def registered_email
    session[:email]
  end
end
