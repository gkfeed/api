from datetime import datetime, date, time, timezone, timedelta
from dateutil.parser import parse


constant_datetime = datetime.combine(
    date(year=2023, month=1, day=21), time(
        hour=8), timezone(offset=timedelta(hours=0))
)


def convert_datetime(date: str) -> datetime:
    return datetime.combine(
        parse(date).date(),
        parse(date).time(),
        timezone(offset=timedelta(hours=0)),
    )
