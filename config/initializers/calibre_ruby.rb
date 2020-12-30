require 'open-uri'

url = URI.join(ENV['CALIBRE_FOLDER_BASE_URL'],'metadata.db').to_s

database_file_name = 'metadata.db'

open(database_file_name , 'wb') do |file|
  file << open(url).read
end

Calibre.db = database_file_name

load Rails.root.join('lib/calibre/book.rb').to_s
