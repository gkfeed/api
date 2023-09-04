from feedgen.feed import FeedGenerator as _FeedGenerator

from app.serializers.feed import Item


class FeedGenerator:
    __title = "MAIN"
    __link = "http://example.com"
    __description = "d"

    @classmethod
    def get_fg(cls) -> _FeedGenerator:
        fg = _FeedGenerator()
        fg.title(cls.__title)
        fg.link(href=cls.__link, rel="alternate")
        fg.description(cls.__description)
        return fg

    @classmethod  # FIXME:
    def create_rss_feed(cls, items: list[Item]) -> str:
        fg = cls.get_fg()

        for item in items:
            cls.__add_item_to_fg(fg, item)

        return fg.rss_str()

    @classmethod
    def __add_item_to_fg(cls, fg: _FeedGenerator, item: Item) -> None:
        fe = fg.add_item()
        fe.title(item.title)
        fe.content(item.text)
        fe.pubDate(item.date)
        fe.link(href=item.link, rel="alternate")
