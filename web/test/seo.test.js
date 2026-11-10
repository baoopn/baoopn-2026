import { test } from "node:test";
import assert from "node:assert/strict";
import {
  blogPostingNode,
  breadcrumbs,
  graph,
  personNode,
  preloadAsset,
  projectNode,
  robotsHeader,
  sameAs,
  websiteNode,
} from "../src/lib/site/seo.js";

const O = "https://baoopn.com";

test("preloadAsset keeps js, css and only the Latin font files", () => {
  const font = (path) => preloadAsset({ type: "font", path });
  assert.equal(font("./_app/immutable/assets/inter-latin-wght-normal.Dx4kXJAl.woff2"), true);
  assert.equal(font("./_app/immutable/assets/outfit-latin-wght-normal.Bc-8i84L.woff2"), true);
  assert.equal(font("./_app/immutable/assets/inter-latin-ext-wght-normal.DO1Apj_S.woff2"), false);
  assert.equal(font("./_app/immutable/assets/inter-vietnamese-wght-normal.CBcvBZtf.woff2"), false);
  assert.equal(font("./_app/immutable/assets/jetbrains-mono-cyrillic-wght-normal.D73BlboJ.woff2"), false);
  assert.equal(preloadAsset({ type: "js", path: "a.js" }), true);
  assert.equal(preloadAsset({ type: "css", path: "a.css" }), true);
  assert.equal(preloadAsset({ type: "asset", path: "a.png" }), false);
});

test("robotsHeader marks only the admin app", () => {
  assert.equal(robotsHeader("/admin"), "noindex, nofollow");
  assert.equal(robotsHeader("/admin/posts/1"), "noindex, nofollow");
  assert.equal(robotsHeader("/administrator"), null);
  assert.equal(robotsHeader("/blog"), null);
});

test("sameAs keeps absolute http(s) links only", () => {
  assert.deepEqual(
    sameAs([{ url: "https://github.com/baoopn" }, { url: "mailto:a@b.c" }, { url: "/x" }, { url: "javascript:1" }]),
    ["https://github.com/baoopn"],
  );
});

test("website and person link by @id and drop empty fields", () => {
  const doc = graph([
    websiteNode(O, { name: "Bao Nguyen", alternateName: "Bao Nguyen" }),
    personNode(O, { name: "Bao Nguyen", sameAs: [] }),
  ]);
  const json = JSON.parse(JSON.stringify(doc));
  assert.equal(json["@context"], "https://schema.org");
  const [site, person] = json["@graph"];
  assert.equal(site.publisher["@id"], `${O}/#person`);
  assert.equal(person["@id"], `${O}/#person`);
  assert.equal("alternateName" in site, false);
  assert.equal("sameAs" in person, false);
  assert.equal("email" in person, false);
});

test("blog part pages point at their own URL and the series", () => {
  const node = blogPostingNode(O, {
    url: `${O}/blog/narraflo/2`,
    headline: "x".repeat(200),
    author: "Bao Nguyen",
    published: "2026-11-07T19:12:27Z",
    series: { url: `${O}/blog/narraflo`, headline: "NarraFlo" },
  });
  assert.equal(node.url, `${O}/blog/narraflo/2`);
  assert.equal(node.mainEntityOfPage, `${O}/blog/narraflo/2`);
  assert.equal(node.isPartOf.url, `${O}/blog/narraflo`);
  assert.equal(node.dateModified, "2026-11-07T19:12:27Z");
  assert.ok(node.headline.length <= 110);
});

test("projects with a repo are SoftwareSourceCode", () => {
  const withRepo = projectNode(O, { url: `${O}/projects/a`, name: "A", author: "Bao Nguyen", repo: "https://github.com/baoopn/a", languages: ["Go"] });
  const without = projectNode(O, { url: `${O}/projects/b`, name: "B", author: "Bao Nguyen", languages: ["Go"] });
  assert.equal(withRepo["@type"], "SoftwareSourceCode");
  assert.deepEqual(withRepo.programmingLanguage, ["Go"]);
  assert.equal(without["@type"], "CreativeWork");
  assert.equal(without.programmingLanguage, undefined);
});

test("breadcrumbs start at Home with absolute item URLs", () => {
  const b = breadcrumbs(O, [["Blog", "/blog"], ["Post", "/blog/p"]]);
  assert.deepEqual(
    b.itemListElement.map((i) => [i.position, i.name, i.item]),
    [[1, "Home", `${O}/`], [2, "Blog", `${O}/blog`], [3, "Post", `${O}/blog/p`]],
  );
});

test("part pages link part 1 by @id and name the author inline", () => {
  const node = blogPostingNode(O, {
    url: `${O}/blog/p/2`,
    headline: "Two",
    author: "Bao Nguyen",
    series: { url: `${O}/blog/p`, headline: "P" },
  });
  assert.equal(node.isPartOf["@id"], `${O}/blog/p#post`);
  assert.equal(node.author.name, "Bao Nguyen");
  assert.equal(node.publisher["@type"], "Person");
});

test("keywords drop case-only repeats", () => {
  const node = projectNode(O, { url: `${O}/projects/a`, name: "A", author: "B", keywords: ["Docker", "docker", "Go", " "] });
  assert.equal(node.keywords, "Docker, Go");
});
