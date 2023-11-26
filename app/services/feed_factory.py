from typing import Optional

from app.services.repositories.feed import FeedRepository
from app.serializers.feed import Feed


class _FeedInput(Feed):
    id: None = None


class FeedFactory:
    @classmethod
    def create_from_url(cls, url: str) -> Optional[Feed]:
        feed_type = cls._try_recognise_feed_type_from_url(url)
        feed_title = cls._recognise_feed_title(url, feed_type)
        feed_url = cls._recognise_feed_url(url, feed_type)

        return FeedRepository.create(
            item=_FeedInput(url=feed_url, type=feed_type, title=feed_title)
        )

    @classmethod
    def _recognise_feed_title(cls, input_url: str, feed_type: str) -> str:
        if feed_type in ("yt", "tiktok"):
            return input_url.split("@")[1].split("/")[0]
        if feed_type == "spoti":
            return input_url.split("/")[-1]
        raise ValueError

    @classmethod
    def _recognise_feed_url(cls, input_url: str, feed_type: str) -> str:
        if feed_type in ("yt", "spoti"):
            return input_url
        if feed_type == "tiktok":
            return "https://www.tiktok.com/@" + input_url.split("@")[1]
        raise ValueError

    @classmethod
    def _try_recognise_feed_type_from_url(cls, url: str) -> str:
        if url.startswith("https://www.youtube.com/@"):
            return "yt"
        if url.startswith("https://tok.adminforge.de/@"):
            return "tiktok"
        if url.startswith("https://open.spotify.com/artist/"):
            return "spoti"
        raise ValueError
