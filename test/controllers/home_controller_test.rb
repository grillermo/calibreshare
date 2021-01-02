require 'test_helper'
require 'pry'

class HomeControllerTest < ActionDispatch::IntegrationTest
  def test_index
    get '/'

    assert_response :success
    assert_select 'h3', Regexp.new(ENV['DEFAULT_EMAIL'])
  end
end
