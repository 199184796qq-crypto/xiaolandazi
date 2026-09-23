# Sample library

## Gold-style seed sample 001

### Source
User-provided reference sample. Use it to learn flow and spoken rhythm. Do not treat its product facts as transferable.

### Original sample

> 总保佑了几十年的皖南土黄鸡，纯种的土鸡，不农药，不圈养，全部在我们安徽皖南的那个高山上面，漫山遍野跑山鸡，溜达鸡，所以这个土鸡的运动量特别大。你们收到货啊，你们会发现这个土鸡的鸡皮特别薄，皮下都是精瘦肉，没有任何的肥肉跟你们扒，肉质呢也比较紧实啊，而且我们这个土鸡呢，在他自己呢，会捕食点小虫的，小竹笋吃不饱，我们喂的是人工科学配比的安全食材，鸡吃的干净，咱们人才能干嘛，吃的放心啊，而且你们现在下单，全部是活鸡，新鲜宰杀，鸡胗、鸡肝、鸡心全部都在。

### Reusable style features

- Starts directly with product identity and origin.
- Keeps one product as the subject for a long continuous stretch.
- Moves through environment → activity → visible/meat features → feeding → slaughter → retained parts.
- Sounds spoken rather than written; uses fillers and self-repair naturally.
- Link/CTA is not the main topic.
- Information is repeated through explanation rather than identical sentences.

### Do not transfer automatically

Product-specific claims including origin, breed, raising method, feeding, meat characteristics, fresh slaughter, and retained offal must come from the current product fact layer.

## Negative pattern 001 — link/parameter fallback

Symptoms:

- “1号是什么、14号是什么” repeated throughout;
- late sections become weight/specification recital;
- forced “对一下链接/总结一下/第一第二”; 
- strong style match at the beginning followed by robotic menu speech.

Root cause learned: exposing the model to a forced 1–7 content-stage outline caused topic switching. Preferred design is one-pass continuous product-story generation followed by runtime-only semantic chunking.
