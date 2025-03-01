"""
AIS IO Datapipe
Copyright (c) 2022-2023, NVIDIA CORPORATION. All rights reserved.
"""

from typing import Iterator, Tuple, List

from torch.utils.data.dataset import T_co
from torchdata.datapipes import functional_datapipe

from torchdata.datapipes.iter import IterDataPipe
from torchdata.datapipes.utils import StreamWrapper

from aistore.sdk.ais_source import AISSource

try:
    from aistore.sdk import Client
    from aistore.pytorch.utils import parse_url, unparse_url

    HAS_AIS = True
except ImportError:
    HAS_AIS = False


def _assert_aistore() -> None:
    if not HAS_AIS:
        raise ModuleNotFoundError(
            "Package `aistore` is required to be installed to use this datapipe."
            "Please run `pip install aistore` or `conda install aistore` to install the package"
            "For more info visit: https://github.com/NVIDIA/aistore/blob/master/python/aistore/"
        )


# pylint: disable=unused-variable
# pylint: disable=W0223
@functional_datapipe("ais_list_files")
class AISFileListerIterDataPipe(IterDataPipe[str]):
    """
    Iterable Datapipe that lists files from the AIStore backends with the given URL prefixes.
        (functional name: ``list_files_by_ais``).
    Acceptable prefixes include but not limited to - `ais://bucket-name`, `ais://bucket-name/`

    Note:
    -   This function also supports files from multiple backends (`aws://..`, `gcp://..`, `hdfs://..`, etc)
    -   Input must be a list and direct URLs are not supported.
    -   length is -1 by default, all calls to len() are invalid as
        not all items are iterated at the start.
    -   This internally uses AIStore Python SDK.

    Args:
        source_datapipe(IterDataPipe[str]): a DataPipe that contains URLs/URL
                                            prefixes to objects on AIS
        length(int): length of the datapipe
        url(str): AIStore endpoint

    Example:
        >>> from torchdata.datapipes.iter import IterableWrapper, AISFileLister
        >>> ais_prefixes = IterableWrapper(['gcp://bucket-name/folder/', 'aws:bucket-name/folder/',
        >>>        'ais://bucket-name/folder/', ...])
        >>> dp_ais_urls = AISFileLister(url='localhost:8080', source_datapipe=ais_prefixes)
        >>> for url in dp_ais_urls:
        ...     pass
        >>> # Functional API
        >>> dp_ais_urls = ais_prefixes.list_files_by_ais(url='localhost:8080')
        >>> for url in dp_ais_urls:
        ...     pass
    """

    def __init__(
        self, source_datapipe: IterDataPipe[str], url: str, length: int = -1
    ) -> None:
        _assert_aistore()
        self.source_datapipe: IterDataPipe[str] = source_datapipe
        self.length: int = length
        self.client = Client(url)

    def __iter__(self) -> Iterator[str]:
        for prefix in self.source_datapipe:
            provider, bck_name, prefix = parse_url(prefix)
            obj_iter = self.client.bucket(bck_name, provider).list_objects_iter(
                prefix=prefix
            )
            for entry in obj_iter:
                yield unparse_url(
                    provider=provider, bck_name=bck_name, obj_name=entry.name
                )

    def __len__(self) -> int:
        if self.length == -1:
            raise TypeError(f"{type(self).__name__} instance doesn't have valid length")
        return self.length


# pylint: disable=unused-variable
# pylint: disable=W0223
@functional_datapipe("ais_load_files")
class AISFileLoaderIterDataPipe(IterDataPipe[Tuple[str, StreamWrapper]]):
    """
    Iterable DataPipe that loads files from AIStore with the given URLs (functional name: ``load_files_by_ais``).
    Iterates all files in BytesIO format and returns a tuple (url, BytesIO).

    Note:
    -   This function also supports files from multiple backends (`aws://..`, `gcp://..`, etc)
    -   Input must be a list and direct URLs are not supported.
    -   This internally uses AIStore Python SDK.
    -   An `etl_name` can be provided to run an existing ETL on the AIS cluster.
        See https://github.com/NVIDIA/aistore/blob/master/docs/etl.md for more info on AIStore ETL.

    Args:
        source_datapipe(IterDataPipe[str]): a DataPipe that contains URLs/URL prefixes to objects
        length(int): length of the datapipe
        url(str): AIStore endpoint
        etl_name (str, optional): Optional etl on the AIS cluster to apply to each object

    Example:
        >>> from torchdata.datapipes.iter import IterableWrapper, AISFileLister,AISFileLoader
        >>> ais_prefixes = IterableWrapper(['gcp://bucket-name/folder/', 'aws:bucket-name/folder/',
        >>>     'ais://bucket-name/folder/', ...])
        >>> dp_ais_urls = AISFileLister(url='localhost:8080', source_datapipe=ais_prefixes)
        >>> dp_cloud_files = AISFileLoader(url='localhost:8080', source_datapipe=dp_ais_urls)
        >>> for url, file in dp_cloud_files:
        ...     pass
        >>> # Functional API
        >>> dp_cloud_files = dp_ais_urls.load_files_by_ais(url='localhost:8080')
        >>> for url, file in dp_cloud_files:
        ...     pass
    """

    def __init__(
        self,
        source_datapipe: IterDataPipe[str],
        url: str,
        length: int = -1,
        etl_name: str = None,
    ) -> None:
        _assert_aistore()
        self.source_datapipe: IterDataPipe[str] = source_datapipe
        self.length = length
        self.client = Client(url)
        self.etl_name = etl_name

    def __iter__(self) -> Iterator[Tuple[str, StreamWrapper]]:
        for url in self.source_datapipe:
            provider, bck_name, obj_name = parse_url(url)
            yield url, StreamWrapper(
                self.client.bucket(bck_name=bck_name, provider=provider)
                .object(obj_name=obj_name)
                .get(etl_name=self.etl_name)
                .raw()
            )

    def __len__(self) -> int:
        return len(self.source_datapipe)


@functional_datapipe("ais_list_sources")
class AISSourceLister(IterDataPipe[str]):
    def __init__(self, ais_sources: List[AISSource], prefix="", etl_name=None):
        """
        Iterable DataPipe over the full URLs for each of the provided AIS source object types

        Args:
            ais_sources (List[AISSource]): List of types implementing the AISSource interface: Bucket, ObjectGroup,
             Object, etc.
            prefix (str, optional): Filter results to only include objects with names starting with this prefix
            etl_name (str, optional): Pre-existing ETL on AIS to apply to all selected objects on the cluster side
        """
        _assert_aistore()
        self.sources = ais_sources
        self.prefix = prefix
        self.etl_name = etl_name

    def __getitem__(self, index) -> T_co:
        raise NotImplementedError

    def __iter__(self) -> Iterator[T_co]:
        for source in self.sources:
            for url in source.list_urls(prefix=self.prefix, etl_name=self.etl_name):
                yield url
