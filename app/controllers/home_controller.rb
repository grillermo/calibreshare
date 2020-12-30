class HomeController < ApplicationController
  before_action do
    if Rails.env.development?
      load Rails.root.join('lib/calibre/book.rb').to_s
    end
  end

  def index
  end

  def register_email
    session[:email] = email_params[:email]

    redirect_to books_path(language: 'spa')
  end

  def book
    redirect_to root_path if not email_registered?

    @book = Calibre::Book.find(book_params[:book_id])
  end

  def books
    redirect_to root_path if not email_registered?

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
