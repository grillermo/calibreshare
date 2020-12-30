# CalibreShare

Your friends input their kindle email and they get to choose from your calibre ebooks with their title, description and cover.

[Demo](http://ebooks.grillermo.com/)

### Technical overview

You upload(or sync) your calibre library folder to a public hosting like S3 or an FTP. 
The app stores a copy of your metadata.db locally and using the calibre-ruby gem extracts the books metadata, asumes the book covers are also stored publicly to display them.
Uses Mailgun to take the chosen file and send it to the user email.

### Deploy your own copy

#### Requisites
- Mailgun account, a free one will do.
- A hosting, you can use digital ocean, but not heroku(they disallow the SQLite gem)
- Your calibre library on a publicly accessible hosting.

#### Installation

1. Clone this project.
2. Customize the .env.example to set the corresponding vars
3. A capfile is provide to deploy using capistrano but you need to customize the `repo_url` cap production deploy

### Thanks

Thanks to the guys of the [calibre-ruby](https://0xacab.org/pip/calibre-ruby) a copy of their repo can be found [here](https://github.com/grillermo/calibre-ruby)
Thanks to the awesome [calibre](https://calibre-ebook.com/) team.


Guillermo Siliceo Trueba

