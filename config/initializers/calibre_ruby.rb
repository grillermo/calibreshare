require 'net/http'
require 'uri'

url = URI.join(ENV.fetch('CALIBRE_FOLDER_BASE_URL', 'http://localhost/'),'metadata.db')

database_file_name = 'metadata.db'

response = Net::HTTP.get_response(url) rescue nil

File.binwrite(database_file_name, response.body) if response && response.is_a?(Net::HTTPSuccess)

Calibre.db = database_file_name

load Rails.root.join('lib/calibre/book.rb').to_s
