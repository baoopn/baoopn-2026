// Lets `node --test workers/inbound-email/test` run every test file: Node 22
// treats the directory argument as a module and loads this file.
import './sign.test.js';
import './rules.test.js';
import './status.test.js';
