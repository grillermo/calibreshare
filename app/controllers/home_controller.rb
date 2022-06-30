class HomeController < ApplicationController
  before_action do
    if Rails.env.development?
      load Rails.root.join('lib/calibre/book.rb').to_s
    end
  end

  before_action :preregister, only: [:books, :book], if: :email_registration_missing?
  before_action :redirect_to_register, only: [:books, :book], if: :email_registration_missing?

  def index
  end

  def register_email
    session[:email] = email_params[:email]

    redirect_to books_path(language: 'spa')
  end

  def book
    @book = Calibre::Book.find(book_params[:book_id])
  end

  def books
    @books = Calibre::Book.list

    @languages = Calibre::Language.all

    if book_params[:language]
      @books = @books.where(languages: {lang_code: book_params[:language]})
    end
  end

  def send_book
    @book  = Calibre::Book.find(book_params[:book_id])

    send_email
  end

  def clear_cache
    Calibre::Book.clear_cache
    session[:email] = nil

    redirect_to books_path(language: 'spa')
  end

  def sources

  end

  private

  def redirect_to_register
    redirect_to root_path
  end

  def preregister
    session[:email] = params[:email] if params[:email].present?
  end

  def send_email
    url   = @book.book_url
    name  = @book.book_file_name
    email = session[:email]

    BookMailer.send_ebook(url, name, email).deliver_later
  end

  def email_params
    params.permit(:email)
  end

  def book_params
    params.permit(:book_id, :language)
  end
end
