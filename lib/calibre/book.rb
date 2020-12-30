class Calibre::Book
  def self.base_url
    ENV['CALIBRE_FOLDER_BASE_URL']
  end

  def self.database_url
    url_for('metadata.db')
  end

  def self.list
    order('books.id DESC').eager_load(:authors, :comments, :data, :languages)
  end

  def self.clear_cache
    database_file_name = 'metadata.db'

    open(database_file_name , 'wb') do |file|
      file << open(database_url).read
    end
  end

  def self.url_for(path)
    URI.join(base_url, URI::encode(path)).to_s
  end

  def image_url
    self.class.url_for(cover)
  end

  def authors_name
    authors.map(&:name).join(', ').gsub('|','')
  end

  def comments_text
    comments.map(&:text).join(', ')
  end

  def book_files
    data
  end

  def book_file
    priority = [
      'mobi',
      'pdf',
      'doc',
      'docx',
      'html',
      'htm',
      'rtf'
    ]

    sorted = book_files.sort_by do |book_file|
      extension = book_file.path.split('.').last

      position = priority.index(extension)

      position || Float::INFINITY
    end

    sorted.first
  end

  def pdf
    book_files.find do |book_file|
      extension = book_file.path.split('.').last

      extension == 'pdf'
    end
  end

  def book_file_name
    book_file.path.split('/')[-1]
  end

  def book_url
    self.class.url_for(book_file.path)
  end

  def preview_url
    self.class.url_for(pdf.path) if pdf
  end

  def language
    languages.first&.lang_code || 'spa'
  end
end
