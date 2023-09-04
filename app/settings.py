import os
import logging

from dotenv import load_dotenv


load_dotenv()

logging.basicConfig(format='%(levelname)s: %(asctime)s: %(message)s')

try:
    DB_URL = os.environ['DB_URL']
except ValueError:
    raise ValueError('Missing configuration')


MODELS = [
    'app.models.feed',
    'app.models.item',
]
