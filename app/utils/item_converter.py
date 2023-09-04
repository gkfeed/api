from rss_parser.models import FeedItem

from app.serializers.feed import Item
from .datetime import convert_datetime


def convert_item(item: FeedItem) -> Item:
    return Item(
        title=item.title,
        text=item.description,
        date=convert_datetime(item.publish_date),
        link=item.link
    )
