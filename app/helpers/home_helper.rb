module HomeHelper
  def language_in_spanish(language)
    if language.lang_code == 'eng'
      "Inglés"
    elsif language.lang_code == 'spa'
      "Español"
    elsif language.lang_code == 'fra'
      "Francés"
    else
     lang_code
    end
  end
end
