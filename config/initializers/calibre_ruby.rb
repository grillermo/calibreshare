require 'http'

url = URI.join(ENV['CALIBRE_FOLDER_BASE_URL'],'metadata.db').to_s

database_file_name = 'metadata.db'

response = HTTP.get(url)

File.binwrite(database_file_name, response.body) if response.status.success?

Calibre.db = database_file_name

load Rails.root.join('lib/calibre/book.rb').to_s
