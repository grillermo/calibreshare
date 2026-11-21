# CalibreShare

Your friends input their kindle email and they get to choose from your calibre ebooks with their title, description and cover.

[Demo](http://ebooks.grillermo.com/)

### Technical overview

You upload (or sync) your calibre library folder to a public hosting like S3 or an FTP.
The app downloads a copy of your `metadata.db` at startup (and on `/clear_cache`), reads the books from it,
and assumes the book covers are also stored publicly to display them.
It uses Mailgun to send the chosen file to the user's email; the file is streamed straight from your
hosting into the Mailgun request.

It is a single Go binary (about 25 MB of RAM) with its templates, styles and images embedded.

### Deploy your own copy

#### Requisites
- Mailgun account, a free one will do.
- Go 1.22 or newer to build it.
- Your calibre library on a publicly accessible hosting.

#### Installation

1. Clone this project.
2. Copy `.env.example` to `.env` and fill it in, or set the variables in your environment.
   `SECRET_KEY_BASE` signs the session cookie; any long random string works (`openssl rand -hex 32`).
3. `go build -o ebooks . && ./ebooks` (listens on `PORT`, default 3000).

Run the tests with `go test ./...`.

### Thanks

Thanks to the guys of the [calibre-ruby](https://0xacab.org/pip/calibre-ruby), which the original Rails version used; a copy of their repo can be found [here](https://github.com/grillermo/calibre-ruby)
Thanks to the awesome [calibre](https://calibre-ebook.com/) team.


Guillermo Siliceo Trueba
