The catalog lives in MongoDB. The first version of this API used Prisma on top of MongoDB, and the old series spent a whole part on a "hybrid" setup with Prisma and raw driver calls. The rewrite uses Mongoose alone. This part covers what the models look like, how search works in two languages, and how view counts turn into "trending" lists.

## Why Mongoose fits this code

Most of the work in this API happens around a save. A product needs clean Vietnamese text and an embedding. A category needs a unique slug. A user needs a hashed password. An order needs a first history event. Mongoose `pre("save")` hooks put each of those rules next to the schema they belong to.

Mongoose also handles TTL indexes, which I use in two places. Refresh tokens drop out when they expire. Product view events drop out after seven days. MongoDB does the delete, so I have no cron job for either.

## The product model

Each product has an English and a Vietnamese copy of its text, plus two fields I never show to users:

```ts
export interface IProduct {
  name: string;
  nameVI: string;
  description: string;
  descriptionVI: string;
  normalizedNameVI: string;
  normalizedDescriptionVI: string;
  price: number;
  views: number;
  recentViews: number;
  featured: boolean;
  thumbnail: string;
  images: string[];
  categoryIds: string[];
  stock: number;
  embedding?: number[];
  // ...timestamps
}
```

`thumbnail` and `images` hold R2 object keys, not full URLs. Part 4 explains why.

Categories have the same English and Vietnamese fields, a `parent` field for nesting, and a slug. The save hook builds the slug from the name and adds `-2`, `-3` and so on until it finds a free one.

## Search in two languages

Vietnamese uses a lot of tone marks. A shopper might type "áo dài" or "ao dai". Both should find the same product.

The fix is a normalized copy of the text. On each save, the hook strips the marks:

```ts
export const normalizeVietnamese = (str: string): string => {
  if (!str) return "";
  return str
    .toLowerCase()
    .replace(/à|á|ạ|ả|ã|â|ầ|ấ|ậ|ẩ|ẫ|ă|ằ|ắ|ặ|ẳ|ẵ/g, "a")
    .replace(/è|é|ẹ|ẻ|ẽ|ê|ề|ế|ệ|ể|ễ/g, "e")
    // ... i, o, u, y
    .replace(/đ/g, "d")
    .replace(/\s+/g, " ")
    .trim();
};
```

The search then runs one case-insensitive pattern across six fields:

```ts
if (search) {
  // Escape special regex characters to prevent injection
  const escapedSearch = search.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");

  filter.$or = [
    { name: { $regex: escapedSearch, $options: "i" } },
    { nameVI: { $regex: escapedSearch, $options: "i" } },
    { normalizedNameVI: { $regex: escapedSearch, $options: "i" } },
    { description: { $regex: escapedSearch, $options: "i" } },
    { descriptionVI: { $regex: escapedSearch, $options: "i" } },
    { normalizedDescriptionVI: { $regex: escapedSearch, $options: "i" } },
  ];
}
```

"áo dài" matches `nameVI`. "ao dai" matches `normalizedNameVI`. English text matches `name`. Escaping the input matters: without it, a search for `.*` would match every product, and a crafted pattern could make MongoDB do a lot of work.

A regex search like this does not use an index. It scans every product. For a small catalog that is fine, and it finds partial words, which MongoDB's text search does not. Both models still define a text index, but no query uses it. I cover that in Part 5.

## Filters, sorts and pages

The same search call takes a price range, category IDs or slugs, `featured`, `inStock`, a sort and a page. Sort fields come from the query string, so I check them against a list:

```ts
const allowedSortFields = [
  "name", "nameVI", "normalizedNameVI", "price",
  "createdAt", "views", "recentViews", "stock", "featured",
];
```

Clients can sort on more than one field, such as `sort=price,name&order=asc,desc`. With no sort, featured products come first, then recent views, then all-time views, then the newest.

The query and the count run side by side with `Promise.all`. Then I attach categories. A product stores category IDs and nothing else about its categories, so I collect the IDs from the whole page and load them in one query:

```ts
const categories = await Category.find({
  _id: { $in: Array.from(allCategoryIds) },
})
  .select("_id name nameVI slug thumbnail")
  .lean();
```

A page of ten products costs three queries, not twelve. Every read also drops the `embedding` field with `.select("-__v -embedding")`. It is 768 numbers per product, and no client needs it.

## Counting views without counting refreshes

The site calls `POST /product/views/:id` when someone opens a product. A plain counter would count every reload. So each view is also saved as an event in a `ProductView` collection, and the service checks for a recent one first:

```ts
const fifteenMinutesAgo = new Date();
fifteenMinutesAgo.setMinutes(fifteenMinutesAgo.getMinutes() - 15);

const recentViewFilter: any = {
  productId: id,
  timestamp: { $gte: fifteenMinutesAgo },
};
if (viewData?.ipAddress) recentViewFilter.ipAddress = viewData.ipAddress;
if (viewData?.userAgent) recentViewFilter.userAgent = viewData.userAgent;

const recentView = await ProductView.findOne(recentViewFilter);
```

If the same IP and browser viewed the product in the last 15 minutes, the count stays the same. If not, the product's `views` goes up by one and a new event is saved. A TTL index removes events after seven days.

## From events to "trending"

`views` is an all-time total. For "trending", I want views from the last seven days. An hourly job rebuilds that number from the events:

```ts
const productViewCounts = await ProductView.aggregate([
  { $match: { timestamp: { $gte: sevenDaysAgo } } },
  { $group: { _id: "$productId", recentViews: { $sum: 1 } } },
]);

// one updateOne per product with views, then:
bulkOps.push({
  updateMany: {
    filter: { _id: { $nin: productIdsWithViews } },
    update: { $set: { recentViews: 0 } },
  },
});

const result = await Product.bulkWrite(bulkOps);
```

The writes go in one `bulkWrite`, so the job makes one trip to the database. Products with no views this week drop to zero. The "popular" list sorts in-stock products by `views`. The "trending" list sorts them by `recentViews`, then by `views`.

## Embeddings, saved for later

The product save hook also sends the four text fields to Google's `text-embedding-004` model and stores a 768-number vector. If the call fails, the hook logs it and saves the product anyway. A missing vector should not block a staff member from adding stock.

Nothing in the API reads the vectors yet. For now they cost one API call per save and some disk space. Each new product gets one when it is created, so a vector search or a "similar products" list could start without a backfill.
