The site works the way I wanted. I write and publish from the dashboard, messages turn into threads, and I can see what people read without a cookie banner. This last part is about what I took away from building it, and what I would do in a different way next time.

![A published blog post with a table of contents and highlighted code blocks](screenshot:public-blog-post)

## What I learned

### One public door makes trust simple

Putting the web server in front of the API, with no second domain, removed a whole class of setup: no CORS and no second certificate. More than that, it made trust explicit. The API believes the client IP and country only when the internal token matches, and the proxy deletes any copy a client tried to send.

The proxy has a cost, and it hides. It strips `Host`, so the API sees its own internal name, not the public one. Any code that needs the public host has to read it from config. The analytics referrer check is one example: compared against `Host`, a visit from my own site would count as an outside referrer. It is a small thing, and a proxy can do this kind of thing in more than one place.

### Sign the field you route on

A webhook signature that covers the body alone is easy to write. The inbound mail webhook picks the thread from the envelope recipient, which arrives as a header. If the HMAC had left that header out, a valid signed request could be moved to another thread. The rule I took from it: whatever field decides where data goes must sit inside the signature.

### Retries need idempotency on the other end

Three places in this system retry: the email worker, the sending mail server behind it, and my own browser when I double-click Send or the network drops. Each retry is safe because the receiving side has a unique key. Inbound mail dedupes on `Message-ID`. Admin replies dedupe on a `request_id` from the browser. With those two `ON CONFLICT` clauses, I stopped worrying about how many times a message might arrive.

### Be conservative with people's words

The quote stripper was the hardest code to get right, and the rule that made it work was a value, not a regex: losing a visitor's words is worse than showing a quote. Every rule removes a line only when it can prove the line came from a mail app. And whenever it removes anything, the full email is kept next to the cleaned one. When the stripper is wrong, nothing is lost.

### A free spam relay is one feature away

Sending an acknowledgement to the address a visitor typed sounds harmless. It is a way for anyone to make my domain send mail to any address. The fix was a stack of limits: no acknowledgement without Turnstile, a fixed text, a name checked for links, and a cap of 30 per hour for the whole site. I now treat any feature that sends mail to an address I did not verify as a security feature first.

### Count only what you can bound

The analytics path allow-list began as a guard against noise, and turned out to be a guard against growth. Every unique path is a new row. Accepting known routes alone keeps the tables bounded, whatever a script sends. The same thinking shows up in the flood limit per thread and the 2 KB cap on beacons.

### Process rules belong in machines

"Never push to main" in a README is a wish. A deploy job that asks GitHub which merged PR a commit came from, and refuses to deploy without one, is a rule. The same goes for the public-safety script in CI. Each one is a short script, and each removed a thing I had to remember.

### Small runtime images have sharp edges

Distroless images are small and have little to attack. They also have no shell, no curl, and no time zone files. That is why the binary has a `healthcheck` subcommand and imports `time/tzdata`. Neither is hard, and both are easy to miss, so I now check for them when I pick a base image.

## What I would change

### State that lives in one process

Several things live in the API's memory: the analytics buffer, the five-minute cache of Clerk users, the site-wide cap on acknowledgements, the ten-minute throttle on reply notices, and the "live now" map. With one API container, that is fine and simple. With two, each would keep its own counts, and the acknowledgement cap would double. If I ever run more than one instance, those need to move into Postgres.

The analytics buffer has a second edge. It flushes every minute and once more on a clean shutdown. A crash loses up to a minute of counts. For a personal site, that is a fair trade for not writing to the database on every page view. For anything bigger I would want a durable queue.

### Bot filtering is a guess

The bot filter is a regex on the user agent plus the `webdriver` flag in the browser. It catches honest bots and common tools. It does not catch a script that sends a normal browser user agent. For my traffic that is good enough, and the path allow-list limits the damage. A more careful version could look at request patterns, but I do not think a portfolio site needs it.

### Daily visitors, not weekly

The daily visitor hash is a privacy choice, and I would make it again. But it means a weekly or monthly "unique visitors" total counts one person once per day. The dashboard should say that in plain words next to the number, so I never read too much into it.

### The quote stripper speaks four languages

Attribution lines are matched in English, French, Spanish and German. A reply from a mail app set to another language keeps its quoted history. That is the safe failure, and the full email is kept anyway, but it shows up as clutter in the thread. Adding languages is easy. Testing them against real mail is the slow part.

## Closing

The biggest change from v3 is not the stack. It is that the site is now a small system I run, with an inbox, a database, and a release process, instead of a set of files I edit. That means more to look after. It also means a post edit is a save in the dashboard, not a code change and a deploy, which is where this whole rebuild started.
