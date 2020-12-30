Rails.application.routes.draw do
  # For details on the DSL available within this file, see http://guides.rubyonrails.org/routing.html
  root 'home#index'

  post '/', to: 'home#index'
  post '/register_email', to: 'home#register_email'

  get '/libros', to: 'home#books', as: :books
  get '/libro/:book_id', to: 'home#book', as: :book
  post '/send_book', to: 'home#send_book'
  get '/send_book', to: redirect('/libros')

  get '/clear_cache', to: 'home#clear_cache'
end
